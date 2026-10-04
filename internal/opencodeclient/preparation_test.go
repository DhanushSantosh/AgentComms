package opencodeclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNativePreparationUsesExactRetainedIdentity(t *testing.T) {
	markerID, err := NewPreparationID()
	if err != nil || !validPreparationID(markerID) {
		t.Fatalf("invalid minted identity: %v", err)
	}
	for _, failure := range []string{"", "post", "identity", "session", "agent", "role", "parts", "missing-parts", "delete", "unconfirmed"} {
		t.Run("failure="+failure, func(t *testing.T) {
			var methods []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				methods = append(methods, r.Method)
				directory, decodeErr := url.PathUnescape(r.Header.Get("x-opencode-directory"))
				if decodeErr != nil || directory != "/owned/project" {
					t.Error("missing exact project routing")
				}
				switch r.Method {
				case http.MethodPost:
					if r.URL.Path != "/session/ses_owned/message" {
						t.Errorf("unexpected creation path %q", r.URL.Path)
					}
					var body struct {
						MessageID string          `json:"messageID"`
						NoReply   bool            `json:"noReply"`
						Tools     map[string]bool `json:"tools"`
						Parts     []TextPart      `json:"parts"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MessageID != markerID || !body.NoReply || len(body.Tools) != 1 || body.Tools["*"] || body.Parts == nil || len(body.Parts) != 0 {
						t.Error("preparation must be exact-ID, no-reply, tool-disabled and empty")
					}
					if failure == "post" {
						http.Error(w, "SYNTHETIC_PRIVATE_MARKER", 500)
						return
					}
					info := PreparationMessage{markerID, "ses_owned", "build", "user"}
					parts := []Part{}
					switch failure {
					case "identity":
						info.ID = "foreign"
					case "session":
						info.SessionID = "ses_foreign"
					case "agent":
						info.Agent = ""
					case "role":
						info.Role = "assistant"
					case "parts":
						parts = []Part{{Type: "text", Text: "unexpected"}}
					case "missing-parts":
						parts = nil
					}
					json.NewEncoder(w).Encode(map[string]any{"info": info, "parts": parts})
				case http.MethodDelete:
					if r.URL.Path != "/session/ses_owned/message/"+markerID {
						t.Error("cleanup targeted a different message")
					}
					if failure == "delete" {
						http.Error(w, "SYNTHETIC_PRIVATE_MARKER", 500)
						return
					}
					json.NewEncoder(w).Encode(failure != "unconfirmed")
				default:
					t.Error("unexpected request")
				}
			}))
			defer server.Close()
			client := New(server.URL, "/owned/project")
			marker, err := client.CreatePreparation(context.Background(), "ses_owned", markerID)
			if err == nil {
				if marker.ID != markerID {
					t.Fatal("lost retained identity")
				}
				err = client.RemovePreparation(context.Background(), marker.SessionID, marker.ID)
			}
			if (err != nil) != (failure != "") {
				t.Fatalf("failure=%s error=%v", failure, err)
			}
			if err != nil && strings.Contains(err.Error(), "SYNTHETIC_PRIVATE_MARKER") {
				t.Fatal("provider response leaked")
			}
			if failure != "" && failure != "delete" && failure != "unconfirmed" && len(methods) != 1 {
				t.Fatal("failed preparation proceeded to cleanup")
			}
		})
	}
}

func TestNativePreparationRejectsUnownedIdentityBeforeRequest(t *testing.T) {
	client := New("http://invalid.invalid", "")
	for _, marker := range []string{"", "msg_foreign", "../message", "msg_" + strings.Repeat("z", 48)} {
		if _, err := client.CreatePreparation(context.Background(), "ses_owned", marker); err == nil {
			t.Fatal("invalid marker accepted")
		}
		if err := client.RemovePreparation(context.Background(), "ses_owned", marker); err == nil {
			t.Fatal("invalid cleanup accepted")
		}
	}
}

func TestNativePreparationInspectionFailsClosed(t *testing.T) {
	markerID, err := NewPreparationID()
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"present", "absent", "server-error", "foreign-id", "foreign-session", "wrong-role", "missing-agent", "content", "missing-parts", "trailing-json", "oversized"} {
		t.Run(variant, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/session/ses_owned/message/"+markerID {
					t.Errorf("inspection escaped exact marker: %s %s", r.Method, r.URL.Path)
				}
				if variant == "absent" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if variant == "server-error" {
					http.Error(w, "PRIVATE_PROVIDER_RESPONSE", http.StatusInternalServerError)
					return
				}
				info := PreparationMessage{markerID, "ses_owned", "build", "user"}
				parts := []Part{}
				switch variant {
				case "foreign-id":
					info.ID = "foreign"
				case "foreign-session":
					info.SessionID = "ses_foreign"
				case "wrong-role":
					info.Role = "assistant"
				case "missing-agent":
					info.Agent = ""
				case "content":
					parts = []Part{{Type: "text", Text: "real conversation"}}
				case "missing-parts":
					parts = nil
				}
				json.NewEncoder(w).Encode(map[string]any{"info": info, "parts": parts})
				if variant == "trailing-json" {
					w.Write([]byte("{}"))
				}
				if variant == "oversized" {
					w.Write([]byte(strings.Repeat(" ", 64*1024) + "{}"))
				}
			}))
			defer server.Close()
			exists, err := New(server.URL, "/owned/project").PreparationExists(context.Background(), "ses_owned", markerID)
			wantError := variant != "present" && variant != "absent"
			if (err != nil) != wantError || exists != (variant == "present") {
				t.Fatalf("exists=%v error=%v", exists, err)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE_PROVIDER_RESPONSE") {
				t.Fatal("provider response leaked")
			}
		})
	}
}
