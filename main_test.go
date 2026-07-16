package main

import (
	"encoding/json"
	"testing"
)

func TestRegistration(t *testing.T) {
	reg := registration()
	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatalf("failed to marshal registration: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("registration marshaled to empty bytes")
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("registration is not valid JSON: %v", err)
	}

	if decoded["schema_version"] == nil {
		t.Fatal("schema_version missing")
	}
}

func TestExtractAPIKey(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "direct apiKey with commandcode file",
			input: `{"apiKey":"user_test"}`,
			want:  "user_test",
		},
		{
			name:  "command-code api key",
			input: `{"command-code":{"type":"api","key":"user_cc"}}`,
			want:  "user_cc",
		},
		{
			name:  "commandcode string",
			input: `{"commandcode":"user_cc2"}`,
			want:  "user_cc2",
		},
		{
			name:    "missing key",
			input:   `{}`,
			wantErr: true,
		},
		{
			name:    "generic apiKey without provider or matching file",
			input:   `{"apiKey":"user_test"}`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filename := "commandcode.json"
			if tc.name == "generic apiKey without provider or matching file" {
				filename = "other.json"
			}
			got, err := extractAPIKey([]byte(tc.input), filename)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("extractAPIKey() = %q, want %q", got, tc.want)
			}
		})
	}
}
