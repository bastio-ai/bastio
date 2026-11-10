package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// Synthetic placeholder data only.
const (
	mapName = "Jane Testperson"
	mapRepl = "[PERSON]"
)

func replaceName(s string) string { return strings.ReplaceAll(s, mapName, mapRepl) }

type mapCase struct {
	name string
	body string
	// mustChange: substrings (JSON-encoded form) expected after rewrite.
	mustContain []string
	// mustKeep: substrings that must survive verbatim (ids, base64, system).
	mustKeep []string
}

func runMapCases(t *testing.T, cases []mapCase, mapper func([]byte, func(string) string) ([]byte, bool), read func([]byte) string) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !json.Valid([]byte(tc.body)) {
				t.Fatalf("fixture is not JSON")
			}
			if !strings.Contains(read([]byte(tc.body)), mapName) {
				t.Fatalf("fixture: reader does not see the name: %q", read([]byte(tc.body)))
			}
			out, changed := mapper([]byte(tc.body), replaceName)
			if !changed {
				t.Fatalf("expected a change")
			}
			if !json.Valid(out) {
				t.Fatalf("output not JSON: %s", out)
			}
			if got := read(out); strings.Contains(got, mapName) {
				t.Errorf("name still visible to the reader: %q", got)
			}
			for _, s := range tc.mustContain {
				if !strings.Contains(string(out), s) {
					t.Errorf("missing %s in %s", s, out)
				}
			}
			for _, s := range tc.mustKeep {
				if !strings.Contains(string(out), s) {
					t.Errorf("%s was altered: %s", s, out)
				}
			}
		})
	}
}

func TestGatewayMapInputText(t *testing.T) {
	const img = `data:image/png;base64,SmFuZSBUZXN0cGVyc29u`
	cases := []mapCase{
		{
			name: "openai chat with tool call and tool result",
			body: `{"model":"gpt-4o","messages":[` +
				`{"role":"system","content":"Assist Jane Testperson's team."},` +
				`{"role":"developer","content":"Jane Testperson is the account owner."},` +
				`{"role":"user","content":[{"type":"text","text":"Find Jane Testperson"},{"type":"image_url","image_url":{"url":"` + img + `"}}]},` +
				`{"role":"assistant","tool_calls":[{"id":"call_Jane_Testperson","type":"function","function":{"name":"lookup","arguments":"{\"who\":\"Jane Testperson\"}"}}]},` +
				`{"role":"tool","tool_call_id":"call_Jane_Testperson","content":"Record: Jane Testperson, tier gold"}]}`,
			mustContain: []string{`"Find [PERSON]"`, `"Record: [PERSON], tier gold"`, `[PERSON]\"`},
			mustKeep:    []string{`"Assist Jane Testperson's team."`, `"Jane Testperson is the account owner."`, `"call_Jane_Testperson"`, img},
		},
		{
			name: "openai responses with function_call_output",
			body: `{"model":"gpt-4o","instructions":"Jane Testperson owns this.","input":[` +
				`{"role":"user","content":[{"type":"input_text","text":"Call Jane Testperson"}]},` +
				`{"type":"function_call","call_id":"fc_Jane_Testperson","name":"lookup","arguments":"{\"q\":\"Jane Testperson\"}"},` +
				`{"type":"function_call_output","call_id":"fc_Jane_Testperson","output":"Found Jane Testperson"}]}`,
			mustContain: []string{`"Call [PERSON]"`, `"Found [PERSON]"`},
			mustKeep:    []string{`"Jane Testperson owns this."`, `"fc_Jane_Testperson"`},
		},
		{
			name: "anthropic messages with tool_use and tool_result",
			body: `{"model":"claude-sonnet-4","system":"Jane Testperson is the operator.","messages":[` +
				`{"role":"user","content":[{"type":"text","text":"Who is Jane Testperson?"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"SmFuZSBUZXN0cGVyc29u"}}]},` +
				`{"role":"assistant","content":[{"type":"tool_use","id":"toolu_Jane_Testperson","name":"lookup","input":{"who":"Jane Testperson"}}]},` +
				`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_Jane_Testperson","content":[{"type":"text","text":"Jane Testperson, account 7"}]}]}]}`,
			mustContain: []string{`"Who is [PERSON]?"`, `"[PERSON], account 7"`, `"who":"[PERSON]"`},
			mustKeep:    []string{`"Jane Testperson is the operator."`, `"toolu_Jane_Testperson"`, `"SmFuZSBUZXN0cGVyc29u"`},
		},
		{
			name: "gemini contents with functionResponse",
			body: `{"systemInstruction":{"parts":[{"text":"Jane Testperson runs support."}]},"contents":[` +
				`{"role":"user","parts":[{"text":"Look up Jane Testperson"},{"inlineData":{"mimeType":"image/png","data":"SmFuZSBUZXN0cGVyc29u"}}]},` +
				`{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"who":"Jane Testperson"}}}]},` +
				`{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"result":"Jane Testperson, tier gold"}}}]}]}`,
			mustContain: []string{`"Look up [PERSON]"`, `"result":"[PERSON], tier gold"`},
			mustKeep:    []string{`"Jane Testperson runs support."`, `"SmFuZSBUZXN0cGVyc29u"`},
		},
	}
	runMapCases(t, cases, GatewayMapInputText, GatewayInputText)
}

func TestGatewayMapOutputText(t *testing.T) {
	cases := []mapCase{
		{
			name:        "openai chat completion with tool call arguments",
			body:        `{"id":"chatcmpl_Jane_Testperson","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"It is Jane Testperson.","tool_calls":[{"id":"call_Jane_Testperson","type":"function","function":{"name":"notify","arguments":"{\"to\":\"Jane Testperson\"}"}}]},"finish_reason":"stop"}]}`,
			mustContain: []string{`"It is [PERSON]."`, `[PERSON]\"`},
			mustKeep:    []string{`"chatcmpl_Jane_Testperson"`, `"call_Jane_Testperson"`},
		},
		{
			name:        "openai responses",
			body:        `{"id":"resp_1","object":"response","output":[{"type":"message","id":"msg_Jane_Testperson","role":"assistant","content":[{"type":"output_text","text":"Hello Jane Testperson"}]}]}`,
			mustContain: []string{`"Hello [PERSON]"`},
			mustKeep:    []string{`"msg_Jane_Testperson"`},
		},
		{
			name:        "anthropic message",
			body:        `{"id":"msg_Jane_Testperson","type":"message","role":"assistant","content":[{"type":"text","text":"Jane Testperson confirmed."}]}`,
			mustContain: []string{`"[PERSON] confirmed."`},
			mustKeep:    []string{`"msg_Jane_Testperson"`},
		},
		{
			name:        "gemini candidates",
			body:        `{"candidates":[{"content":{"role":"model","parts":[{"text":"Jane Testperson is listed."}]},"finishReason":"STOP"}],"responseId":"Jane_Testperson_1"}`,
			mustContain: []string{`"[PERSON] is listed."`},
			mustKeep:    []string{`"Jane_Testperson_1"`},
		},
	}
	runMapCases(t, cases, GatewayMapOutputText, GatewayOutputText)
}

func TestGatewayMapText_MalformedAndUnchanged(t *testing.T) {
	malformed := []byte(`{"messages":[{"role":"user","content":"Jane Testperson"`)
	if out, changed := GatewayMapInputText(malformed, replaceName); changed || string(out) != string(malformed) {
		t.Errorf("malformed input must be returned unchanged with changed=false")
	}
	if out, changed := GatewayMapOutputText(malformed, replaceName); changed || string(out) != string(malformed) {
		t.Errorf("malformed output must be returned unchanged with changed=false")
	}
	sse := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"Jane Testperson\"}}]}\n\n")
	if _, changed := GatewayMapOutputText(sse, replaceName); changed {
		t.Errorf("SSE bodies must not be rewritten")
	}
	clean := []byte(`{"messages":[{"role":"user","content":"nothing to see"}]}`)
	if out, changed := GatewayMapInputText(clean, replaceName); changed || string(out) != string(clean) {
		t.Errorf("unchanged body must be returned as is")
	}
}
