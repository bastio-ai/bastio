package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"

	"github.com/bastio-ai/bastio/internal/providers"
)

// walkBatchJSONL accepts a final line without a newline. Reader.ReadSlice is
// bounded per item instead of Scanner's 64 KiB default or unbounded ReadBytes.
func walkBatchJSONL(reader io.Reader, write io.Writer, apply func([]byte, int) ([]byte, error)) error {
	limited := &io.LimitedReader{R: reader, N: batchFileLimit + 1}
	buf := bufio.NewReaderSize(limited, 64<<10)
	var line []byte
	index := 0
	for {
		chunk, err := buf.ReadSlice('\n')
		if len(line)+len(chunk) > batchLineLimit {
			return &routeError{413, "batch JSONL item exceeds 64 MiB"}
		}
		line = append(line, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if limited.N <= 0 {
			return &routeError{413, "batch file exceeds 2 GiB"}
		}
		if len(line) > 0 {
			if !json.Valid(line) {
				return &routeError{400, fmt.Sprintf("invalid batch JSONL at line %d", index+1)}
			}
			if index >= batchItemLimit {
				return &routeError{413, "batch exceeds 100000 items"}
			}
			next, e := apply(line, index)
			if e != nil {
				return e
			}
			if _, e = write.Write(next); e != nil {
				return e
			}
			if len(next) == 0 || next[len(next)-1] != '\n' {
				if _, e = write.Write([]byte{'\n'}); e != nil {
					return e
				}
			}
			index++
			line = line[:0]
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if index == 0 {
		return &routeError{400, "batch JSONL must contain at least one item"}
	}
	return nil
}

func (b *batchCall) inputJSONL(reader io.Reader, write io.Writer, manifest *batchManifest) error {
	seen := map[string]bool{}
	return walkBatchJSONL(reader, write, func(line []byte, index int) ([]byte, error) {
		var obj map[string]json.RawMessage
		if json.Unmarshal(line, &obj) != nil || obj == nil {
			return nil, &routeError{400, "batch JSONL must contain objects"}
		}
		id, field := batchString(obj, "custom_id"), "body"
		if b.route.provider == providers.ProviderGemini {
			id, field = batchString(obj, "key"), "request"
		}
		if id == "" || len(id) > 256 || seen[id] {
			return nil, &routeError{400, fmt.Sprintf("batch line %d requires a unique ID of 1 to 256 bytes", index+1)}
		}
		seen[id] = true
		if b.route.provider == providers.ProviderOpenAI {
			method, endpoint := batchString(obj, "method"), batchString(obj, "url")
			if method != "POST" || !batchAllowedOpenAIEndpoint(endpoint) {
				return nil, &routeError{400, "batch JSONL requires a supported POST /v1 endpoint"}
			}
			if manifest.Endpoint != "" && manifest.Endpoint != endpoint {
				return nil, &routeError{400, "OpenAI batch items must use the same endpoint"}
			}
			manifest.Endpoint = endpoint
		}
		next, err := b.inputItem(obj[field], batchHash(id), manifest)
		if err != nil {
			return nil, err
		}
		obj[field] = next
		return json.Marshal(obj)
	})
}
func batchAllowedOpenAIEndpoint(path string) bool {
	switch path {
	case "/v1/responses", "/v1/chat/completions", "/v1/embeddings", "/v1/completions", "/v1/moderations", "/v1/images/generations", "/v1/images/edits":
		return true
	}
	return false
}

func (b *batchCall) openAIUpload(w http.ResponseWriter) error {
	b.tags["batch.operation"] = "upload"
	media, params, err := mime.ParseMediaType(b.r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" || params["boundary"] == "" {
		return &routeError{400, "files upload requires multipart/form-data"}
	}
	reader := multipart.NewReader(b.r.Body, params["boundary"])
	type field struct{ name, value string }
	var fields []field
	var filename string
	var fileContentType string
	source, err := batchSpool()
	if err != nil {
		return err
	}
	defer removeBatchSpool(source)
	haveFile := false
	purpose := ""
	for {
		part, e := reader.NextPart()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return &routeError{400, "invalid multipart upload"}
		}
		if part.FormName() == "file" {
			if haveFile {
				return &routeError{400, "files upload accepts exactly one file"}
			}
			haveFile = true
			filename = filepath.Base(part.FileName())
			fileContentType = part.Header.Get("Content-Type")
			n, e := io.Copy(source, io.LimitReader(part, batchFileLimit+1))
			if e != nil {
				return e
			}
			if n > 200<<20 {
				return &routeError{413, "OpenAI batch files are limited to 200 MiB"}
			}
		} else {
			value, e := io.ReadAll(io.LimitReader(part, 64<<10+1))
			if e != nil {
				return e
			}
			if len(value) > 64<<10 || len(fields) >= 32 {
				return &routeError{413, "multipart metadata too large"}
			}
			fields = append(fields, field{part.FormName(), string(value)})
			if part.FormName() == "purpose" {
				purpose = string(value)
			}
		}
		_ = part.Close()
	}
	if !haveFile || purpose == "" {
		return &routeError{400, "files upload requires file and purpose"}
	}
	if _, err = rewindBatch(source); err != nil {
		return err
	}
	resource := batchResource{Manifest: b.newManifest(), Role: "opaque"}
	payload := source
	if purpose == "batch" {
		payload, err = batchSpool()
		if err != nil {
			return err
		}
		defer removeBatchSpool(payload)
		if err = b.inputJSONL(source, payload, &resource.Manifest); err != nil {
			return err
		}
		resource.Role = "input"
	}
	if _, err = rewindBatch(payload); err != nil {
		return err
	}
	outbound, err := batchSpool()
	if err != nil {
		return err
	}
	defer removeBatchSpool(outbound)
	writer := multipart.NewWriter(outbound)
	for _, field := range fields {
		if err = writer.WriteField(field.name, field.value); err != nil {
			return err
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filename}))
	if purpose == "batch" {
		fileContentType = "application/jsonl"
	}
	if fileContentType == "" {
		fileContentType = "application/octet-stream"
	}
	header.Set("Content-Type", fileContentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err = io.Copy(part, payload); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	length, err := rewindBatch(outbound)
	if err != nil {
		return err
	}
	headers := b.r.Header.Clone()
	headers.Set("Content-Type", writer.FormDataContentType())
	resp, err := b.forward("POST", "/files", nil, outbound, length, headers)
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
	id := batchString(obj, "id")
	if id == "" {
		return &routeError{502, "provider returned no file ID"}
	}
	if err = b.put("file", id, resource); err != nil {
		return err
	}
	b.status = uint16(resp.StatusCode)
	copyProviderResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, err = w.Write(raw)
	return err
}

func (b *batchCall) bindOutputFiles(obj map[string]json.RawMessage, resource batchResource) error {
	files := []string{batchString(obj, "output_file_id"), batchString(obj, "error_file_id")}
	if b.route.provider == providers.ProviderGemini {
		var response map[string]json.RawMessage
		_ = json.Unmarshal(obj["response"], &response)
		files = append(files, batchString(response, "responsesFile", "responses_file"))
		// Some Google batch API revisions wrap the destination in output.
		var output map[string]json.RawMessage
		_ = json.Unmarshal(response["output"], &output)
		files = append(files, batchString(output, "responsesFile", "responses_file"))
		var metadata map[string]json.RawMessage
		_ = json.Unmarshal(obj["metadata"], &metadata)
		_ = json.Unmarshal(metadata["output"], &output)
		files = append(files, batchString(output, "responsesFile", "responses_file"))
	}
	for _, file := range files {
		if file == "" {
			continue
		}
		copy := resource
		copy.Role = "output"
		if err := b.put("file", file, copy); err != nil {
			return err
		}
	}
	return nil
}
func (b *batchCall) isResults(path string) bool {
	return b.route.provider == providers.ProviderOpenAI && strings.HasPrefix(path, "/files/") && strings.HasSuffix(path, "/content") || b.route.provider == providers.ProviderAnthropic && strings.HasPrefix(path, "/messages/batches/") && strings.HasSuffix(path, "/results") || b.route.provider == providers.ProviderGemini && (strings.HasPrefix(path, "/download/") || strings.HasPrefix(path, "/files/") && (strings.HasSuffix(path, ":download") || b.r.URL.Query().Get("alt") == "media"))
}
func (b *batchCall) downloadResults(w http.ResponseWriter, path string) error {
	b.tags["batch.operation"] = "download"
	if b.r.Method != http.MethodGet {
		return &routeError{405, "batch result download requires GET"}
	}
	if b.r.Header.Get("Range") != "" {
		return &routeError{400, "range downloads are not supported for inspected batch results"}
	}
	resourcePath := path
	if b.route.provider == providers.ProviderGemini {
		resourcePath = strings.TrimPrefix(path, "/download/v1beta")
		resourcePath = strings.TrimSuffix(resourcePath, ":download")
	}
	id, kind := batchResourcePath(b.route.provider, resourcePath)
	resource, err := b.get(kind, id)
	if err != nil {
		return err
	}
	upstreamPath := path
	if b.route.provider == providers.ProviderGemini && strings.HasPrefix(path, "/files/") {
		upstreamPath = "/download/v1beta" + strings.TrimSuffix(path, ":download") + ":download"
	}
	resp, err := b.forward("GET", upstreamPath, nil, nil, 0, b.r.Header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return b.copyError(w, resp)
	}
	if resource.Role == "opaque" {
		b.tags["batch.coverage"] = "binary_not_inspected"
		b.status = uint16(resp.StatusCode)
		copyProviderResponseHeaders(w.Header(), resp.Header)
		w.Header().Set("X-Bastio-Content-Coverage", "binary-not-inspected")
		w.WriteHeader(resp.StatusCode)
		_, err = io.Copy(w, resp.Body)
		return err
	}
	spool, err := batchSpool()
	if err != nil {
		return err
	}
	defer removeBatchSpool(spool)
	seen := map[string]bool{}
	err = walkBatchJSONL(resp.Body, spool, func(line []byte, index int) ([]byte, error) {
		var item map[string]json.RawMessage
		if json.Unmarshal(line, &item) != nil || item == nil {
			return nil, &routeError{502, "provider batch results must contain JSON objects"}
		}
		id := batchString(item, "custom_id", "key")
		key := batchHash(id)
		if b.route.provider == providers.ProviderGemini && id == "" && index < len(resource.Manifest.Keys) {
			key = resource.Manifest.Keys[index]
		}
		if resource.Role == "input" {
			return b.storedInputItem(line)
		}
		if !resource.Manifest.keySet[key] {
			if id == "" && (bytes.Contains(item["error"], []byte("{")) || bytes.Contains(item["result"], []byte(`"errored"`))) {
				key = batchHash(fmt.Sprintf("#error-%d", index))
			} else {
				return nil, &routeError{502, "provider batch result correlation ID does not match the submitted input"}
			}
		}
		if seen[key] {
			return nil, &routeError{502, "duplicate correlation ID in batch results"}
		}
		seen[key] = true
		return b.outputItem(line, key, resource)
	})
	if err != nil {
		var policy *routeError
		if errors.As(err, &policy) && (policy.status == 400 || policy.status == 413) {
			return &routeError{502, "provider batch result is malformed or exceeds inspection limits"}
		}
		return err
	}
	if _, err = rewindBatch(spool); err != nil {
		return err
	}
	b.tags["batch.job_id"] = resource.Job
	b.status = uint16(resp.StatusCode)
	copyProviderResponseHeaders(w.Header(), resp.Header)
	for _, header := range []string{"ETag", "Last-Modified", "Accept-Ranges", "Content-Range"} {
		w.Header().Del(header)
	}
	w.Header().Set("Content-Type", "application/jsonl")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Bastio-Content-Coverage", "text-inspected")
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, spool)
	return err
}

func (b *batchCall) basePath() string {
	if pos := strings.Index(b.r.URL.Path, "/guard/"); pos >= 0 {
		tail := b.r.URL.Path[pos:]
		if cut := strings.Index(tail, "/v1/"); cut >= 0 {
			return b.r.URL.Path[:pos] + tail[:cut] + "/v1"
		}
	}
	switch b.route.provider {
	case providers.ProviderAnthropic:
		return "/v1/anthropic/v1"
	case providers.ProviderGemini:
		return "/v1/google/v1beta"
	default:
		return "/v1/openai"
	}
}
func (b *batchCall) publicURL(path string) string {
	return b.publicOrigin() + b.basePath() + path
}
func (b *batchCall) rewriteBatchLinks(obj map[string]json.RawMessage) {
	if _, ok := obj["results_url"]; ok {
		id := batchString(obj, "id")
		if id != "" {
			obj["results_url"], _ = json.Marshal(b.publicURL("/messages/batches/" + id + "/results"))
		}
	}
	// Google's downloadUri is a transport URL; uri is a provider model file
	// reference and must remain unchanged so generateContent can consume it.
	for _, key := range []string{"downloadUri", "download_uri"} {
		if _, ok := obj[key]; ok {
			name := batchString(obj, "name")
			if strings.HasPrefix(name, "files/") {
				obj[key], _ = json.Marshal(b.publicURL("/download/v1beta/" + name + ":download?alt=media"))
			}
		}
	}
	for _, key := range []string{"data", "operations", "files"} {
		var items []map[string]json.RawMessage
		if json.Unmarshal(obj[key], &items) == nil && items != nil {
			for _, item := range items {
				b.rewriteBatchLinks(item)
			}
			obj[key], _ = json.Marshal(items)
		}
	}
}
