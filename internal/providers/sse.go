package providers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// SSEFrame preserves the wire event alongside parsed fields. Keeping Raw
// avoids losing native events, comments, retry fields or multiline payloads.
type SSEFrame struct {
	Raw      []byte
	Data     []byte
	Event    string
	ID       string
	Comments []string
}

// ReadSSE accepts LF/CRLF, multiline data and a final event without a blank
// line. emit runs synchronously, so backpressure and cancellation propagate.
func ReadSSE(ctx context.Context, reader io.Reader, emit func(SSEFrame) error) error {
	r := bufio.NewReader(reader)
	var frame SSEFrame
	var data []string
	flush := func() error {
		if len(frame.Raw) == 0 {
			return nil
		}
		frame.Data = []byte(strings.Join(data, "\n"))
		err := emit(frame)
		frame, data = SSEFrame{}, nil
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			if len(frame.Raw)+len(line) > 16<<20 {
				return fmt.Errorf("sse event exceeds maximum size")
			}
			frame.Raw = append(frame.Raw, line...)
			content := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if content == "" {
				if flushErr := flush(); flushErr != nil {
					return flushErr
				}
			} else {
				field, value, _ := strings.Cut(content, ":")
				value = strings.TrimPrefix(value, " ")
				switch field {
				case "":
					frame.Comments = append(frame.Comments, value)
				case "data":
					data = append(data, value)
				case "event":
					frame.Event = value
				case "id":
					if !strings.ContainsRune(value, '\x00') {
						frame.ID = value
					}
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				return err
			}
			return flush()
		}
	}
}

// RewriteSSEData changes only data lines, retaining native event metadata.
func RewriteSSEData(frame SSEFrame, payload []byte) []byte {
	var out strings.Builder
	for _, line := range strings.Split(string(frame.Raw), "\n") {
		if strings.HasPrefix(line, "data:") || strings.TrimSpace(line) == "" {
			continue
		}
		out.WriteString(strings.TrimSuffix(line, "\r"))
		out.WriteByte('\n')
	}
	for _, line := range strings.Split(string(payload), "\n") {
		out.WriteString("data: ")
		out.WriteString(line)
		out.WriteByte('\n')
	}
	out.WriteByte('\n')
	return []byte(out.String())
}
