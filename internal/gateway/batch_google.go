package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/bastio-ai/bastio/internal/batchstate"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/google/uuid"
)

func googleObject(obj map[string]json.RawMessage, camel, snake string) (map[string]json.RawMessage, string) {
	key := camel
	if _, ok := obj[key]; !ok {
		key = snake
	}
	var child map[string]json.RawMessage
	_ = json.Unmarshal(obj[key], &child)
	return child, key
}
func (b *batchCall) prepareGoogleCreate(path string, obj map[string]json.RawMessage, resource batchResource) ([]byte, batchResource, error) {
	batch, _ := googleObject(obj, "batch", "batch")
	config, configKey := googleObject(batch, "inputConfig", "input_config")
	if config == nil {
		return nil, resource, &routeError{400, "Google batch requires batch.inputConfig"}
	}
	model := strings.TrimPrefix(strings.Split(strings.TrimPrefix(path, "/models/"), ":")[0], "models/")
	resource.Manifest.Model = model
	if file := batchString(config, "fileName", "file_name"); file != "" {
		known, err := b.get("file", file)
		if err != nil {
			return nil, resource, err
		}
		if known.Role != "input" {
			return nil, resource, &routeError{400, "Google batch input must be a JSONL file uploaded through this gateway"}
		}
		resource.Manifest = known.Manifest
		resource.Manifest.Model = model
		resource.Manifest.TraceID = b.trace.String()
		out, _ := json.Marshal(obj)
		return out, resource, nil
	}
	requests, requestsKey := googleObject(config, "requests", "requests")
	itemsKey := "requests"
	if strings.HasSuffix(path, ":asyncBatchEmbedContent") {
		itemsKey = "requests"
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(requests[itemsKey], &items) != nil || len(items) == 0 || len(items) > batchItemLimit {
		return nil, resource, &routeError{400, "Google inline batch requires 1 to 100000 requests"}
	}
	seen := map[string]bool{}
	for i, item := range items {
		meta, _ := googleObject(item, "metadata", "metadata")
		id := batchString(meta, "key")
		if id == "" {
			id = fmt.Sprintf("#%d", i)
		}
		key := batchHash(id)
		if seen[key] {
			return nil, resource, &routeError{400, "Google batch metadata keys must be unique"}
		}
		seen[key] = true
		raw, err := b.inputItem(item["request"], key, &resource.Manifest)
		if err != nil {
			return nil, resource, err
		}
		item["request"] = raw
	}
	requests[itemsKey], _ = json.Marshal(items)
	config[requestsKey], _ = json.Marshal(requests)
	batch[configKey], _ = json.Marshal(config)
	obj["batch"], _ = json.Marshal(batch)
	out, _ := json.Marshal(obj)
	return out, resource, nil
}

// mapGoogleContent visits textual model content and structured tool data while
// preserving file URIs, media bytes, schemas, IDs and thought signatures.
func mapGoogleContent(raw []byte, transform textTransform) ([]byte, bool) {
	return mapGoogleContentMode(raw, transform, false)
}
func mapGoogleContentMode(raw []byte, transform textTransform, skipSigned bool) ([]byte, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return raw, false
	}
	if skipSigned && batchString(obj, "thoughtSignature", "thought_signature") != "" {
		return raw, false
	}
	changed := false
	if value, ok := obj["error"]; ok {
		var details map[string]json.RawMessage
		if json.Unmarshal(value, &details) == nil {
			for _, key := range []string{"message", "details"} {
				if value, ok := details[key]; ok {
					next, ch := mapDataStrings(value, transform)
					if ch {
						details[key] = next
						changed = true
					}
				}
			}
			if changed {
				obj["error"], _ = json.Marshal(details)
			}
		}
	}
	if value, ok := obj["text"]; ok {
		var text string
		if json.Unmarshal(value, &text) == nil {
			next := transform(text)
			if next != text {
				obj["text"], _ = json.Marshal(next)
				changed = true
			}
		}
	}
	for _, key := range []string{"contents", "content", "parts", "candidates", "response", "request", "output"} {
		value, ok := obj[key]
		if !ok {
			continue
		}
		var list []json.RawMessage
		if json.Unmarshal(value, &list) == nil && list != nil {
			ch := false
			for i, v := range list {
				next, c := mapGoogleContentMode(v, transform, skipSigned)
				list[i] = next
				ch = ch || c
			}
			if ch {
				obj[key], _ = json.Marshal(list)
				changed = true
			}
		} else {
			next, ch := mapGoogleContentMode(value, transform, skipSigned)
			if ch {
				obj[key] = next
				changed = true
			}
		}
	}
	for _, key := range []string{"functionCall", "function_call", "functionResponse", "function_response"} {
		value, ok := obj[key]
		if !ok {
			continue
		}
		var fn map[string]json.RawMessage
		if json.Unmarshal(value, &fn) == nil {
			ch := false
			for _, data := range []string{"args", "response"} {
				if v, exists := fn[data]; exists {
					next, c := mapDataStrings(v, transform)
					fn[data] = next
					ch = ch || c
				}
			}
			if ch {
				obj[key], _ = json.Marshal(fn)
				changed = true
			}
		}
	}
	if !changed {
		return raw, false
	}
	out, _ := json.Marshal(obj)
	return out, true
}

func (b *batchCall) googleInlineResults(raw []byte, resource batchResource) ([]byte, error) {
	if resource.Manifest.keySet == nil {
		resource.Manifest.keySet = make(map[string]bool, len(resource.Manifest.Keys))
		for _, id := range resource.Manifest.Keys {
			resource.Manifest.keySet[id] = true
		}
	}
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(raw, &obj)
	changed := false
	// Operations return results under response; metadata.output can carry the
	// same object on some revisions, so process it with the same policy.
	for _, outer := range []string{"response", "metadata"} {
		parent, _ := googleObject(obj, outer, outer)
		if parent == nil {
			continue
		}
		// Metadata may echo submitted inline inputs. Those are also data
		// returned to a caller and must honor today's output policy.
		config, configKey := googleObject(parent, "inputConfig", "input_config")
		if config != nil {
			requests, requestKey := googleObject(config, "requests", "requests")
			var inputs []map[string]json.RawMessage
			if requests != nil && json.Unmarshal(requests["requests"], &inputs) == nil {
				for _, item := range inputs {
					next, err := b.storedInput(item["request"])
					if err != nil {
						return nil, err
					}
					item["request"] = next
				}
				requests["requests"], _ = json.Marshal(inputs)
				config[requestKey], _ = json.Marshal(requests)
				parent[configKey], _ = json.Marshal(config)
				changed = true
			}
		}
		targets := []string{""}
		if _, exists := parent["output"]; exists {
			targets = append(targets, "output")
		}
		for _, target := range targets {
			result := parent
			if target != "" {
				result, _ = googleObject(parent, target, target)
			}
			key := "inlinedResponses"
			if _, exists := result["inlinedEmbedContentResponses"]; exists {
				key = "inlinedEmbedContentResponses"
			}
			value, ok := result[key]
			if !ok {
				key = "inlined_responses"
				value, ok = result[key]
			}
			if !ok {
				continue
			}
			var items []json.RawMessage
			var wrapper map[string]json.RawMessage
			innerKey := key
			if json.Unmarshal(value, &items) != nil {
				if json.Unmarshal(value, &wrapper) != nil {
					return nil, &routeError{502, "malformed Google inline batch results"}
				}
				if _, exists := wrapper[innerKey]; !exists {
					innerKey = "inlinedResponses"
				}
				if json.Unmarshal(wrapper[innerKey], &items) != nil {
					return nil, &routeError{502, "malformed Google inline batch results"}
				}
			}
			if len(items) > len(resource.Manifest.Keys) {
				return nil, &routeError{502, "Google batch returned more results than input requests"}
			}
			for i, item := range items {
				next, err := b.outputItem(item, resource.Manifest.Keys[i], resource)
				if err != nil {
					return nil, err
				}
				items[i] = next
			}
			next, _ := json.Marshal(items)
			if wrapper != nil {
				wrapper[innerKey] = next
				next, _ = json.Marshal(wrapper)
			}
			result[key] = next
			if target != "" {
				parent[target], _ = json.Marshal(result)
			}
			changed = true
		}
		obj[outer], _ = json.Marshal(parent)
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(obj)
}

type googleUploadState struct {
	Metadata json.RawMessage `json:"metadata"`
	Size     int64           `json:"size"`
	MIME     string          `json:"mime"`
	Response json.RawMessage `json:"response,omitempty"`
}

func (b *batchCall) publicOrigin() string {
	if origin := os.Getenv("BASTIO_PUBLIC_API_URL"); origin != "" {
		return strings.TrimRight(origin, "/")
	}
	scheme := "https"
	if strings.HasPrefix(b.r.Host, "localhost:") || strings.HasPrefix(b.r.Host, "127.0.0.1:") || strings.HasPrefix(b.r.Host, "[::1]:") {
		scheme = "http"
	}
	return scheme + "://" + b.r.Host
}
func (b *batchCall) googleUpload(w http.ResponseWriter, path string) error {
	b.tags["batch.operation"] = "upload"
	if b.r.Method != http.MethodPost {
		return &routeError{405, "resumable uploads require POST"}
	}
	command := strings.ToLower(strings.ReplaceAll(b.r.Header.Get("X-Goog-Upload-Command"), " ", ""))
	if strings.HasPrefix(path, "/upload/") {
		if command != "start" || !strings.EqualFold(b.r.Header.Get("X-Goog-Upload-Protocol"), "resumable") {
			return &routeError{400, "Google files require the resumable upload protocol"}
		}
		size, err := strconv.ParseInt(b.r.Header.Get("X-Goog-Upload-Header-Content-Length"), 10, 64)
		if err != nil || size < 0 || size > batchFileLimit {
			return &routeError{413, "Google upload size must be between 0 and 2 GiB"}
		}
		metadata, err := readBatchJSON(b.r.Body, 1<<20)
		if err != nil {
			return err
		}
		mime := b.r.Header.Get("X-Goog-Upload-Header-Content-Type")
		if mime == "" {
			mime = "application/octet-stream"
		}
		state := googleUploadState{Metadata: metadata, Size: size, MIME: mime}
		raw, _ := json.Marshal(state)
		id := uuid.NewString()
		if err = b.h.batchStore.CreateUpload(b.r.Context(), b.key("upload", id), raw, size); err != nil {
			if errors.Is(err, batchstate.ErrCapacity) {
				return &routeError{429, "temporary batch upload capacity unavailable; finalize or cancel existing uploads"}
			}
			return err
		}
		w.Header().Set("X-Goog-Upload-URL", b.publicURL("/batch-uploads/"+id))
		w.Header().Set("X-Goog-Upload-Status", "active")
		w.Header().Set("X-Goog-Upload-Chunk-Granularity", "1")
		w.WriteHeader(200)
		return nil
	}
	id := strings.TrimPrefix(path, "/batch-uploads/")
	if _, err := uuid.Parse(id); err != nil {
		return &routeError{404, "upload session not found"}
	}
	key := b.key("upload", id)
	record, err := b.h.batchStore.Get(b.r.Context(), key)
	if errors.Is(err, batchstate.ErrMissing) {
		return &routeError{404, "upload session unavailable or expired"}
	}
	if err != nil {
		return err
	}
	var state googleUploadState
	if json.Unmarshal(record.Payload, &state) != nil {
		return &routeError{502, "invalid encrypted upload state"}
	}
	status := "active"
	if record.Completed {
		status = "final"
	}
	w.Header().Set("X-Goog-Upload-Status", status)
	w.Header().Set("X-Goog-Upload-Size-Received", strconv.FormatInt(record.Received, 10))
	if command == "query" {
		w.WriteHeader(200)
		return nil
	}
	if command == "cancel" {
		if err = b.h.batchStore.Delete(b.r.Context(), key); err != nil {
			return err
		}
		w.Header().Set("X-Goog-Upload-Status", "cancelled")
		w.WriteHeader(200)
		return nil
	}
	if command != "upload" && command != "upload,finalize" && command != "finalize" {
		return &routeError{400, "unsupported resumable upload command"}
	}
	if record.Completed {
		if strings.Contains(command, "finalize") {
			w.Header().Set("Content-Type", "application/json")
			_, err = w.Write(state.Response)
			return err
		}
		return &routeError{409, "upload already finalized"}
	}
	offset, err := strconv.ParseInt(b.r.Header.Get("X-Goog-Upload-Offset"), 10, 64)
	if err != nil || offset != record.Received {
		return &routeError{409, "upload offset does not match received bytes; query the session before retrying"}
	}
	if strings.HasPrefix(command, "upload") {
		chunk := make([]byte, 2<<20)
		for {
			n, e := io.ReadFull(b.r.Body, chunk)
			if n > 0 {
				if offset+int64(n) > state.Size {
					return &routeError{413, "upload exceeds declared file size"}
				}
				offset, err = b.h.batchStore.Append(b.r.Context(), key, offset, chunk[:n])
				if errors.Is(err, batchstate.ErrOffset) {
					return &routeError{409, "upload offset changed; query session before retrying"}
				}
				if err != nil {
					return err
				}
			}
			if e == io.EOF || e == io.ErrUnexpectedEOF {
				break
			}
			if e != nil {
				return e
			}
		}
	}
	w.Header().Set("X-Goog-Upload-Size-Received", strconv.FormatInt(offset, 10))
	if !strings.Contains(command, "finalize") {
		w.WriteHeader(200)
		return nil
	}
	if offset != state.Size {
		return &routeError{400, "cannot finalize an incomplete upload"}
	}
	leased, err := b.h.batchStore.Lease(b.r.Context(), key)
	if err != nil {
		return err
	}
	if !leased {
		return &routeError{409, "upload finalization already in progress"}
	}
	defer func() { _ = b.h.batchStore.Release(b.r.Context(), key) }()
	input := &batchChunkReader{b: b, key: key, size: state.Size}
	payload, err := batchSpool()
	if err != nil {
		return err
	}
	defer removeBatchSpool(payload)
	resource := batchResource{Manifest: b.newManifest(), Role: "opaque"}
	if strings.Contains(state.MIME, "jsonl") || strings.Contains(state.MIME, "ndjson") || state.MIME == "application/json" {
		if err = b.inputJSONL(input, payload, &resource.Manifest); err != nil {
			return err
		}
		resource.Role = "input"
	} else {
		if _, err = io.Copy(payload, input); err != nil {
			return err
		}
	}
	length, err := rewindBatch(payload)
	if err != nil {
		return err
	}
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Goog-Upload-Protocol", "resumable")
	headers.Set("X-Goog-Upload-Command", "start")
	headers.Set("X-Goog-Upload-Header-Content-Length", strconv.FormatInt(length, 10))
	headers.Set("X-Goog-Upload-Header-Content-Type", state.MIME)
	start, err := b.forward("POST", "/upload/v1beta/files", state.Metadata, nil, 0, headers)
	if err != nil {
		return err
	}
	defer start.Body.Close()
	if start.StatusCode < 200 || start.StatusCode >= 300 {
		return b.copyError(w, start)
	}
	uploadURL := start.Header.Get("X-Goog-Upload-URL")
	if uploadURL == "" {
		return &routeError{502, "Google returned no resumable upload URL"}
	}
	if _, err = url.ParseRequestURI(uploadURL); err != nil {
		return &routeError{502, "invalid provider upload URL"}
	}
	headers = make(http.Header)
	headers.Set("Content-Type", state.MIME)
	headers.Set("X-Goog-Upload-Offset", "0")
	headers.Set("X-Goog-Upload-Command", "upload, finalize")
	resp, err := b.client.Forward(b.r.Context(), &providers.NativeRequest{Method: "POST", Path: "/upload/v1beta/files", Headers: headers, BodyReader: payload, ContentLength: length, UploadURL: uploadURL}, b.route.key)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return b.copyError(w, resp)
	}
	raw, err := readBatchJSON(resp.Body, 1<<20)
	if err != nil {
		return err
	}
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(raw, &obj)
	file, _ := googleObject(obj, "file", "file")
	name := batchString(file, "name")
	if !strings.HasPrefix(name, "files/") {
		return &routeError{502, "Google returned no file name"}
	}
	if err = b.put("file", name, resource); err != nil {
		return err
	}
	b.rewriteBatchLinks(file)
	obj["file"], _ = json.Marshal(file)
	raw, _ = json.Marshal(obj)
	state.Response = raw
	complete, _ := json.Marshal(state)
	if err = b.h.batchStore.Complete(b.r.Context(), key, complete); err != nil {
		return err
	}
	b.status = uint16(resp.StatusCode)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Goog-Upload-Status", "final")
	w.WriteHeader(resp.StatusCode)
	_, err = w.Write(raw)
	return err
}

type batchChunkReader struct {
	b      *batchCall
	key    batchstate.Key
	offset int64
	size   int64
	buffer *bytes.Reader
}

func (r *batchChunkReader) Read(p []byte) (int, error) {
	if r.buffer == nil || r.buffer.Len() == 0 {
		if r.offset == r.size {
			return 0, io.EOF
		}
		chunk, err := r.b.h.batchStore.Chunk(r.b.r.Context(), r.key, r.offset)
		if errors.Is(err, io.EOF) {
			return 0, io.ErrUnexpectedEOF
		}
		if err != nil {
			return 0, err
		}
		if len(chunk) == 0 || r.offset+int64(len(chunk)) > r.size {
			return 0, io.ErrUnexpectedEOF
		}
		r.offset += int64(len(chunk))
		r.buffer = bytes.NewReader(chunk)
	}
	return r.buffer.Read(p)
}
