package sourcesettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/app/metadataextract"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/modelapi"
)

type extractionModelStub struct {
	capabilities, calls         int
	chatCapabilities, chatCalls int
	after                       func()
	err                         error
}

func (m *extractionModelStub) LlamaCPPChatCapability(context.Context, string, credentials.Secret, string) (modelapi.ExtractionCapability, error) {
	m.chatCapabilities++
	return modelapi.ExtractionCapability{Fingerprint: "synthetic-chat", Protocol: modelapi.ProtocolLlamaCPPChat, BudgetMode: modelapi.BudgetModeVerifiedResponse}, m.err
}
func (m *extractionModelStub) ExtractLlamaCPPChat(context.Context, string, credentials.Secret, string, string, json.RawMessage, int, modelapi.ExtractionCapability) (json.RawMessage, error) {
	m.chatCalls++
	if m.after != nil {
		m.after()
	}
	return json.RawMessage(`{"fields":{}}`), m.err
}

func (*extractionModelStub) Models(context.Context, string, credentials.Secret) (modelapi.ModelsResult, error) {
	return modelapi.ModelsResult{}, nil
}
func (*extractionModelStub) Infer(context.Context, string, credentials.Secret, string, string, json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}
func (m *extractionModelStub) ExtractionCapability(context.Context, string, credentials.Secret, string) (modelapi.ExtractionCapability, error) {
	m.capabilities++
	return modelapi.ExtractionCapability{ContextTokens: 4096, Fingerprint: "synthetic", Protocol: modelapi.ProtocolLlamaCPPNative, BudgetMode: modelapi.BudgetModeExactTokens}, m.err
}
func (m *extractionModelStub) Extract(context.Context, string, credentials.Secret, string, string, json.RawMessage, int, modelapi.ExtractionCapability) (json.RawMessage, error) {
	m.calls++
	if m.after != nil {
		m.after()
	}
	return json.RawMessage(`{"fields":{}}`), m.err
}
func extractionCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed *metadataextract.Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("want %s got %v", code, err)
	}
}

func TestExtractionSnapshotDisabledAndMissingConfigurationMakeNoNetworkCalls(t *testing.T) {
	s, repo := newTestService()
	m := &extractionModelStub{}
	s.models = m
	_, err := s.Snapshot(context.Background(), 0)
	extractionCode(t, err, "not_configured")
	// A disabled record need not decrypt retired credentials or have a target.
	repo.records[AIProviderID] = Record{ProviderID: AIProviderID, ConfigVersion: 1, Config: json.RawMessage(`{}`), Envelope: credentials.Envelope{Ciphertext: []byte("corrupt-retired-key")}}
	_, err = s.Snapshot(context.Background(), 1)
	extractionCode(t, err, "disabled")
	r := repo.records[AIProviderID]
	r.Enabled = true
	r.Config = json.RawMessage(`{"base_url":"http://localhost/v1"}`)
	repo.records[AIProviderID] = r
	_, err = s.Snapshot(context.Background(), 1)
	extractionCode(t, err, "not_configured")
	if m.capabilities != 0 || m.calls != 0 || m.chatCapabilities != 0 || m.chatCalls != 0 {
		t.Fatal("invalid or disabled configuration contacted model")
	}
}
func TestExtractionProtocolSelectsOneAdapterAndPreservesCAS(t *testing.T) {
	for _, protocol := range []string{"", modelapi.ProtocolLlamaCPPNative, modelapi.ProtocolLlamaCPPChat} {
		t.Run("protocol="+protocol, func(t *testing.T) {
			s, repo := newTestService()
			m := &extractionModelStub{}
			s.models = m
			update := AIUpdate{ExpectedVersion: pointer(int64(0)), Enabled: pointer(true), BaseURL: pointer("http://localhost/v1"), ModelID: pointer("exact-model"), Credential: CredentialChange{Action: "replace", Value: "synthetic-secret-private"}}
			if protocol != "" {
				update.Protocol = pointer(protocol)
			}
			config, err := s.SaveAI(context.Background(), update)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := s.Snapshot(context.Background(), uint64(config.ConfigVersion))
			if err != nil {
				t.Fatal(err)
			}
			wantProtocol, wantMode, wantContext := modelapi.ProtocolLlamaCPPNative, modelapi.BudgetModeExactTokens, 4096
			if protocol == modelapi.ProtocolLlamaCPPChat {
				wantProtocol, wantMode, wantContext = protocol, modelapi.BudgetModeVerifiedResponse, 0
			}
			if snapshot.Protocol != wantProtocol || snapshot.BudgetMode != wantMode || snapshot.ContextTokens != wantContext || !snapshot.BudgetVerified || snapshot.CapabilityFingerprint == "" {
				t.Fatal("wrong saved protocol capability")
			}
			altered := snapshot
			altered.BudgetMode = "unsupported-budget-mode"
			_, err = s.ExtractJSON(context.Background(), altered, "must not send", json.RawMessage(`{}`), 128)
			extractionCode(t, err, "config_changed")
			m.after = func() { r := repo.records[AIProviderID]; r.ConfigVersion++; repo.records[AIProviderID] = r }
			_, err = s.ExtractJSON(context.Background(), snapshot, "synthetic text", json.RawMessage(`{}`), 128)
			extractionCode(t, err, "config_changed")
			if protocol == modelapi.ProtocolLlamaCPPChat {
				if m.chatCapabilities != 1 || m.chatCalls != 1 || m.capabilities != 0 || m.calls != 0 {
					t.Fatal("chat protocol fell back to native")
				}
			} else if m.capabilities != 1 || m.calls != 1 || m.chatCapabilities != 0 || m.chatCalls != 0 {
				t.Fatal("native protocol fell back to chat")
			}
		})
	}
}
func TestExtractionUnknownProtocolRejectedBeforeDecryption(t *testing.T) {
	s, repo := newTestService()
	m := &extractionModelStub{}
	s.models = m
	repo.records[AIProviderID] = Record{ProviderID: AIProviderID, Enabled: true, ConfigVersion: 1, Config: json.RawMessage(`{"protocol":"unknown","base_url":"http://localhost/v1","model_id":"exact-model"}`), Envelope: credentials.Envelope{Ciphertext: []byte("corrupt-retired-key")}}
	_, err := s.Snapshot(context.Background(), 1)
	extractionCode(t, err, "schema_unsupported")
	r := repo.records[AIProviderID]
	r.Enabled = false
	r.Config = json.RawMessage(`{"protocol":"llama_cpp_chat"}`)
	repo.records[AIProviderID] = r
	_, err = s.Snapshot(context.Background(), 1)
	extractionCode(t, err, "disabled")
	if m.capabilities != 0 || m.calls != 0 || m.chatCapabilities != 0 || m.chatCalls != 0 {
		t.Fatal("invalid or disabled protocol contacted model")
	}
}
func TestExtractionSavedSnapshotProtectsVersionAndSecret(t *testing.T) {
	s, repo := newTestService()
	m := &extractionModelStub{}
	s.models = m
	config, err := s.SaveAI(context.Background(), AIUpdate{ExpectedVersion: pointer(int64(0)), Enabled: pointer(true), BaseURL: pointer("http://localhost/v1"), ModelID: pointer("exact-model"), Credential: CredentialChange{Action: "replace", Value: "synthetic-secret-private"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot(context.Background(), uint64(config.ConfigVersion))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(snapshot)
	if strings.Contains(string(data), "synthetic-secret-private") || strings.Contains(fmt.Sprintf("%+v", snapshot), "synthetic-secret-private") {
		t.Fatal("snapshot exposed credential")
	}
	m.after = func() {
		r := repo.records[AIProviderID]
		r.ConfigVersion++
		r.Enabled = false
		repo.records[AIProviderID] = r
	}
	_, err = s.ExtractJSON(context.Background(), snapshot, "synthetic input", json.RawMessage(`{}`), 128)
	extractionCode(t, err, "config_changed")
	_, err = s.ExtractJSON(context.Background(), snapshot, "must not send", json.RawMessage(`{}`), 128)
	extractionCode(t, err, "config_changed")
	if m.calls != 1 {
		t.Fatal("retired snapshot sent text")
	}
}
func TestExtractionErrorCategoriesAreFinite(t *testing.T) {
	for _, sample := range []struct {
		err  error
		code string
	}{{context.Canceled, "cancelled"}, {context.DeadlineExceeded, "timeout"}, {&modelapi.Error{Code: "unsupported"}, "schema_unsupported"}, {&modelapi.Error{Code: "response_too_large"}, "invalid_response"}, {&modelapi.Error{Code: "private-unknown-code"}, "unreachable"}, {errors.New("private upstream body"), "unreachable"}} {
		extractionCode(t, extractionError(sample.err), sample.code)
	}
}
