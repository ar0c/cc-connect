package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type namedTestAgent struct {
	name string
}

func (a *namedTestAgent) Name() string { return a.name }
func (a *namedTestAgent) StartSession(_ context.Context, _ string) (AgentSession, error) {
	return &stubAgentSession{}, nil
}
func (a *namedTestAgent) ListSessions(_ context.Context) ([]AgentSessionInfo, error) { return nil, nil }
func (a *namedTestAgent) Stop() error                                                { return nil }

// mockChannelResolver implements both Platform and ChannelNameResolver.
type mockChannelResolver struct {
	name  string
	names map[string]string
}

func (m *mockChannelResolver) Name() string {
	if m.name != "" {
		return m.name
	}
	return "mock"
}
func (m *mockChannelResolver) Start(MessageHandler) error                     { return nil }
func (m *mockChannelResolver) Reply(_ context.Context, _ any, _ string) error { return nil }
func (m *mockChannelResolver) Send(_ context.Context, _ any, _ string) error  { return nil }
func (m *mockChannelResolver) Stop() error                                    { return nil }
func (m *mockChannelResolver) ResolveChannelName(channelID string) (string, error) {
	if name, ok := m.names[channelID]; ok {
		return name, nil
	}
	return "", fmt.Errorf("unknown channel %s", channelID)
}

func newTestEngineWithMultiWorkspace(t *testing.T, baseDir string) *Engine {
	t.Helper()
	tmpDir := t.TempDir()
	bindingPath := filepath.Join(tmpDir, "bindings.json")
	e := NewEngine("test", nil, nil, "", LangEnglish)
	e.SetMultiWorkspace(baseDir, bindingPath)
	return e
}

func newTestEngineWithMultiWorkspaceAgent(t *testing.T, baseDir string) *Engine {
	t.Helper()
	tmpDir := t.TempDir()
	bindingPath := filepath.Join(tmpDir, "bindings.json")
	sessionPath := filepath.Join(tmpDir, "sessions.json")
	agentName := "shared-binding-test-agent"
	RegisterAgent(agentName, func(opts map[string]any) (Agent, error) {
		return &namedTestAgent{name: agentName}, nil
	})
	e := NewEngine("test", &namedTestAgent{name: agentName}, nil, sessionPath, LangEnglish)
	e.SetMultiWorkspace(baseDir, bindingPath)
	return e
}

func TestMultiWorkspaceResolution_ConventionMatch(t *testing.T) {
	baseDir := t.TempDir()
	channelName := "my-project"
	channelID := "C001"

	// Create a directory matching the channel name
	if err := os.MkdirAll(filepath.Join(baseDir, channelName), 0o755); err != nil {
		t.Fatal(err)
	}

	e := newTestEngineWithMultiWorkspace(t, baseDir)
	p := &mockChannelResolver{names: map[string]string{channelID: channelName}}

	ws, name, err := e.resolveWorkspace(p, channelID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != channelName {
		t.Errorf("expected channel name %q, got %q", channelName, name)
	}
	// resolveWorkspace returns normalizeWorkspacePath'd result; use it for comparison
	expectedWS := normalizeWorkspacePath(filepath.Join(baseDir, channelName))
	if ws != expectedWS {
		t.Errorf("expected workspace %q, got %q", expectedWS, ws)
	}

	// Verify auto-binding was persisted
	b := e.workspaceBindings.Lookup("project:test", workspaceChannelKey(p.Name(), channelID))
	if b == nil {
		t.Fatal("expected binding to be created by convention match")
	}
	if b.Workspace != expectedWS {
		t.Errorf("binding workspace = %q, want %q", b.Workspace, expectedWS)
	}
}

func TestMultiWorkspaceResolution_NoMatch(t *testing.T) {
	baseDir := t.TempDir() // empty directory — no convention match possible

	e := newTestEngineWithMultiWorkspace(t, baseDir)
	p := &mockChannelResolver{names: map[string]string{"C002": "nonexistent-project"}}

	ws, name, err := e.resolveWorkspace(p, "C002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != "" {
		t.Errorf("expected empty workspace, got %q", ws)
	}
	if name != "nonexistent-project" {
		t.Errorf("expected channel name %q, got %q", "nonexistent-project", name)
	}
}

func TestMultiWorkspaceResolution_ExistingBinding(t *testing.T) {
	baseDir := t.TempDir()
	channelID := "C003"
	channelName := "bound-channel"

	// Create the workspace directory the binding points to
	wsDir := filepath.Join(baseDir, "some-workspace")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := newTestEngineWithMultiWorkspace(t, baseDir)
	e.workspaceBindings.Bind("project:test", channelID, channelName, wsDir)

	// Platform that does NOT know this channel — binding should still work
	p := &mockChannelResolver{names: map[string]string{}}

	ws, name, err := e.resolveWorkspace(p, channelID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// resolveWorkspace normalizes the path
	expectedWS := normalizeWorkspacePath(wsDir)
	if ws != expectedWS {
		t.Errorf("expected workspace %q, got %q", expectedWS, ws)
	}
	if name != channelName {
		t.Errorf("expected channel name %q, got %q", channelName, name)
	}
}

func TestMultiWorkspaceResolution_SharedBinding(t *testing.T) {
	baseDir := t.TempDir()
	channelID := "C003S"
	channelName := "shared-channel"

	wsDir := filepath.Join(baseDir, "shared-workspace")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := newTestEngineWithMultiWorkspace(t, baseDir)
	e.workspaceBindings.Bind(sharedWorkspaceBindingsKey, channelID, channelName, wsDir)

	p := &mockChannelResolver{names: map[string]string{}}

	ws, name, err := e.resolveWorkspace(p, channelID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedWS := normalizeWorkspacePath(wsDir)
	if ws != expectedWS {
		t.Errorf("expected workspace %q, got %q", expectedWS, ws)
	}
	if name != channelName {
		t.Errorf("expected channel name %q, got %q", channelName, name)
	}
}

func TestMultiWorkspaceResolution_SharedBindingDoesNotCrossPlatforms(t *testing.T) {
	baseDir := t.TempDir()
	channelID := "C003X"

	wsDir := filepath.Join(baseDir, "shared-workspace")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := newTestEngineWithMultiWorkspace(t, baseDir)
	e.workspaceBindings.Bind(sharedWorkspaceBindingsKey, workspaceChannelKey("mock-a", channelID), "shared-channel", wsDir)

	pA := &mockChannelResolver{name: "mock-a", names: map[string]string{}}
	pB := &mockChannelResolver{name: "mock-b", names: map[string]string{}}

	ws, _, err := e.resolveWorkspace(pA, channelID)
	if err != nil {
		t.Fatalf("unexpected error for matching platform: %v", err)
	}
	if ws != normalizeWorkspacePath(wsDir) {
		t.Fatalf("expected shared binding for matching platform, got %q", ws)
	}

	ws, name, err := e.resolveWorkspace(pB, channelID)
	if err != nil {
		t.Fatalf("unexpected error for other platform: %v", err)
	}
	if ws != "" || name != "" {
		t.Fatalf("expected no shared binding for other platform, got workspace=%q channelName=%q", ws, name)
	}
}

func TestMultiWorkspaceResolution_MissingDirRemovesBinding(t *testing.T) {
	baseDir := t.TempDir()
	channelID := "C004"
	channelName := "stale-channel"
	missingDir := filepath.Join(baseDir, "deleted-workspace")

	e := newTestEngineWithMultiWorkspace(t, baseDir)
	e.workspaceBindings.Bind("project:test", channelID, channelName, missingDir)

	p := &mockChannelResolver{names: map[string]string{}}

	ws, name, err := e.resolveWorkspace(p, channelID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != "" {
		t.Errorf("expected empty workspace for missing dir, got %q", ws)
	}
	if name != channelName {
		t.Errorf("expected channel name %q, got %q", channelName, name)
	}

	// Verify binding was removed
	if b := e.workspaceBindings.Lookup("project:test", channelID); b != nil {
		t.Error("expected binding to be removed after missing directory")
	}
}

func TestMultiWorkspaceResolution_MissingDirKeepsSharedBinding(t *testing.T) {
	baseDir := t.TempDir()
	channelID := "C004S"
	channelName := "shared-stale-channel"
	missingDir := filepath.Join(baseDir, "deleted-shared-workspace")

	e := newTestEngineWithMultiWorkspace(t, baseDir)
	e.workspaceBindings.Bind(sharedWorkspaceBindingsKey, channelID, channelName, missingDir)

	p := &mockChannelResolver{names: map[string]string{}}

	ws, name, err := e.resolveWorkspace(p, channelID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != "" {
		t.Errorf("expected empty workspace for missing dir, got %q", ws)
	}
	if name != channelName {
		t.Errorf("expected channel name %q, got %q", channelName, name)
	}
	if b := e.workspaceBindings.Lookup(sharedWorkspaceBindingsKey, channelID); b == nil {
		t.Error("expected shared binding to remain after missing directory")
	}
}

func TestInteractiveKeyForSessionKey_MissingSharedBindingFallsBack(t *testing.T) {
	baseDir := t.TempDir()
	channelID := "C005SM"
	missingDir := filepath.Join(baseDir, "missing-shared-workspace")

	e := newTestEngineWithMultiWorkspace(t, baseDir)
	e.workspaceBindings.Bind(sharedWorkspaceBindingsKey, channelID, "shared-channel", missingDir)

	sessionKey := "mock:" + channelID + ":user"
	if got := e.interactiveKeyForSessionKey(sessionKey); got != sessionKey {
		t.Fatalf("interactiveKeyForSessionKey() = %q, want %q", got, sessionKey)
	}
}

func TestInteractiveKeyForSessionKey_SharedBinding(t *testing.T) {
	baseDir := t.TempDir()
	channelID := "C005S"
	wsDir := filepath.Join(baseDir, "shared-workspace")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := newTestEngineWithMultiWorkspace(t, baseDir)
	e.workspaceBindings.Bind(sharedWorkspaceBindingsKey, channelID, "shared-channel", wsDir)

	sessionKey := "mock:" + channelID + ":user"
	want := normalizeWorkspacePath(wsDir) + ":" + sessionKey
	if got := e.interactiveKeyForSessionKey(sessionKey); got != want {
		t.Fatalf("interactiveKeyForSessionKey() = %q, want %q", got, want)
	}
}

func TestSessionContextForKey_MissingSharedBindingFallsBack(t *testing.T) {
	baseDir := t.TempDir()
	channelID := "C006SM"
	missingDir := filepath.Join(baseDir, "missing-shared-workspace")

	e := newTestEngineWithMultiWorkspaceAgent(t, baseDir)
	e.workspaceBindings.Bind(sharedWorkspaceBindingsKey, channelID, "shared-channel", missingDir)

	agent, sessions := e.sessionContextForKey("mock:" + channelID + ":user")
	if agent != e.agent {
		t.Fatal("expected base agent for missing shared binding")
	}
	if sessions != e.sessions {
		t.Fatal("expected base session manager for missing shared binding")
	}
	if got := e.workspacePool.Get(normalizeWorkspacePath(missingDir)); got != nil {
		t.Fatal("did not expect workspace pool entry for missing shared binding")
	}
}

func TestSessionContextForKey_SharedBinding(t *testing.T) {
	baseDir := t.TempDir()
	channelID := "C006S"
	wsDir := filepath.Join(baseDir, "shared-workspace")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := newTestEngineWithMultiWorkspaceAgent(t, baseDir)
	e.workspaceBindings.Bind(sharedWorkspaceBindingsKey, channelID, "shared-channel", wsDir)

	agent, sessions := e.sessionContextForKey("mock:" + channelID + ":user")
	if agent == nil {
		t.Fatal("expected workspace agent, got nil")
	}
	if agent == e.agent {
		t.Fatal("expected workspace-specific agent, got base agent")
	}
	if sessions == nil {
		t.Fatal("expected workspace session manager, got nil")
	}
	if sessions == e.sessions {
		t.Fatal("expected workspace session manager, got base session manager")
	}
	if got := e.workspacePool.Get(normalizeWorkspacePath(wsDir)); got == nil || got.agent == nil || got.sessions == nil {
		t.Fatal("expected workspace pool entry to be created for shared binding")
	}
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/org/my-repo.git", "my-repo"},
		{"https://github.com/org/my-repo", "my-repo"},
		{"git@github.com:org/my-repo.git", "my-repo"},
		{"git@github.com:org/my-repo", "my-repo"},
		{"https://gitlab.com/group/subgroup/project.git", "project"},
		{"ssh://git@github.com/org/repo.git", "repo"},
		{"https://github.com/org/repo", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractRepoName(tt.input)
			if got != tt.want {
				t.Errorf("extractRepoName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLooksLikeGitURL(t *testing.T) {
	valid := []string{
		"https://github.com/org/repo",
		"http://github.com/org/repo",
		"git@github.com:org/repo.git",
		"ssh://git@github.com/org/repo",
	}
	for _, s := range valid {
		if !looksLikeGitURL(s) {
			t.Errorf("looksLikeGitURL(%q) = false, want true", s)
		}
	}

	invalid := []string{
		"not-a-url",
		"ftp://files.example.com/repo",
		"/local/path/to/repo",
		"",
		"github.com/org/repo",
	}
	for _, s := range invalid {
		if looksLikeGitURL(s) {
			t.Errorf("looksLikeGitURL(%q) = true, want false", s)
		}
	}
}

func TestWorkspaceInitFlow_SlashCommandCleansUpExistingFlow(t *testing.T) {
	baseDir := t.TempDir()
	e := newTestEngineWithMultiWorkspace(t, baseDir)
	p := &mockChannelResolver{names: map[string]string{"C010": "test-channel"}}

	channelID := "C010"
	channelKey := workspaceChannelKey(p.Name(), channelID)

	// Seed a flow in "awaiting_url" state to simulate a prior regular message
	// that triggered the init flow.
	e.initFlowsMu.Lock()
	e.initFlows[channelKey] = &workspaceInitFlow{
		state:       "awaiting_url",
		channelName: "test-channel",
	}
	e.initFlowsMu.Unlock()

	msg := &Message{SessionKey: "mock:" + channelID + ":user1", Content: "/workspace bind my-project"}

	consumed := e.handleWorkspaceInitFlow(p, msg, "test-channel")
	if consumed {
		t.Fatal("expected handleWorkspaceInitFlow to return false for slash command, but it returned true")
	}

	// Verify the flow was cleaned up.
	e.initFlowsMu.Lock()
	_, stillExists := e.initFlows[channelKey]
	e.initFlowsMu.Unlock()
	if stillExists {
		t.Error("expected init flow to be deleted after slash command, but it still exists")
	}
}

func TestSeedChannelBindings_CreatesBindings(t *testing.T) {
	baseDir := t.TempDir()

	// Create workspace directories
	backendDir := filepath.Join(baseDir, "backend")
	frontendDir := filepath.Join(baseDir, "frontend")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(frontendDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := newTestEngineWithMultiWorkspace(t, baseDir)

	bindings := map[string]string{
		"1111111111": backendDir,
		"2222222222": frontendDir,
	}
	e.SeedChannelBindings(bindings)

	// Verify bindings are queryable via resolveWorkspace
	// (they should exist as project-level bindings)
	projectKey := "project:" + e.name
	for channelID, wsDir := range bindings {
		channelKey := workspaceChannelKey("discord", channelID)
		b := e.workspaceBindings.Lookup(projectKey, channelKey)
		if b == nil {
			t.Fatalf("expected binding for channel %s, got nil", channelID)
		}
		expected := normalizeWorkspacePath(wsDir)
		if b.Workspace != expected {
			t.Errorf("channel %s: workspace = %q, want %q", channelID, b.Workspace, expected)
		}
	}
}

func TestSeedChannelBindings_DoesNotOverwriteExisting(t *testing.T) {
	baseDir := t.TempDir()

	wsDir := filepath.Join(baseDir, "backend")
	overrideDir := filepath.Join(baseDir, "override")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := newTestEngineWithMultiWorkspace(t, baseDir)

	// Simulate a user's /dir binding already in place
	channelID := "1111111111"
	projectKey := "project:" + e.name
	channelKey := workspaceChannelKey("discord", channelID)
	e.workspaceBindings.Bind(projectKey, channelKey, "backend", normalizeWorkspacePath(overrideDir))

	// Seed should NOT overwrite the existing binding
	e.SeedChannelBindings(map[string]string{
		channelID: wsDir,
	})

	b := e.workspaceBindings.Lookup(projectKey, channelKey)
	if b == nil {
		t.Fatal("expected binding, got nil")
	}
	if b.Workspace != normalizeWorkspacePath(overrideDir) {
		t.Errorf("seed overwrote existing binding: got %q, want %q", b.Workspace, normalizeWorkspacePath(overrideDir))
	}
}

func TestSeedChannelBindings_SkipsMissingWorkspace(t *testing.T) {
	baseDir := t.TempDir()

	e := newTestEngineWithMultiWorkspace(t, baseDir)

	// Seed with a non-existent directory
	e.SeedChannelBindings(map[string]string{
		"1111111111": filepath.Join(baseDir, "does-not-exist"),
	})

	projectKey := "project:" + e.name
	channelKey := workspaceChannelKey("discord", "1111111111")
	b := e.workspaceBindings.Lookup(projectKey, channelKey)
	if b != nil {
		t.Errorf("expected no binding for missing workspace, got %+v", b)
	}
}

func TestSeedChannelBindings_ExpandsTilde(t *testing.T) {
	baseDir := t.TempDir()
	wsDir := filepath.Join(baseDir, "myproject")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := newTestEngineWithMultiWorkspace(t, baseDir)

	// Use tilde path — SeedChannelBindings should expand ~/ to home dir
	home, _ := os.UserHomeDir()
	tildeWs := "~/" + "nonexistent-tilde-test"
	realWs := filepath.Join(home, "nonexistent-tilde-test")

	// Create the directory so the binding succeeds
	if err := os.MkdirAll(realWs, 0o755); err != nil {
		t.Skip("cannot create dir in home for tilde test")
	}
	defer os.RemoveAll(realWs)

	e.SeedChannelBindings(map[string]string{
		"3333333333": tildeWs,
	})

	projectKey := "project:" + e.name
	channelKey := workspaceChannelKey("discord", "3333333333")
	b := e.workspaceBindings.Lookup(projectKey, channelKey)
	if b == nil {
		t.Fatal("expected binding for tilde-expanded path, got nil")
	}
	if b.Workspace != normalizeWorkspacePath(realWs) {
		t.Errorf("workspace = %q, want %q", b.Workspace, normalizeWorkspacePath(realWs))
	}
}

func TestSeedChannelBindings_ResolvesRelativeToBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	wsDir := filepath.Join(baseDir, "myproject")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := newTestEngineWithMultiWorkspace(t, baseDir)

	// Relative path should resolve against base_dir
	e.SeedChannelBindings(map[string]string{
		"4444444444": "myproject",
	})

	projectKey := "project:" + e.name
	channelKey := workspaceChannelKey("discord", "4444444444")
	b := e.workspaceBindings.Lookup(projectKey, channelKey)
	if b == nil {
		t.Fatal("expected binding for relative path, got nil")
	}
	if b.Workspace != normalizeWorkspacePath(wsDir) {
		t.Errorf("workspace = %q, want %q", b.Workspace, normalizeWorkspacePath(wsDir))
	}
}

func TestSeedChannelBindings_NilMapIsNoop(t *testing.T) {
	baseDir := t.TempDir()
	e := newTestEngineWithMultiWorkspace(t, baseDir)

	// Should not panic
	e.SeedChannelBindings(nil)
	e.SeedChannelBindings(map[string]string{})
}

func TestSeedChannelBindings_SetsAllowedChannels(t *testing.T) {
	baseDir := t.TempDir()
	for _, d := range []string{"backend", "frontend"} {
		if err := os.MkdirAll(filepath.Join(baseDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	e := newTestEngineWithMultiWorkspace(t, baseDir)
	e.SeedChannelBindings(map[string]string{
		"1111": "backend",
		"2222": "frontend",
	})

	if !e.IsChannelAllowed("1111") {
		t.Error("expected channel 1111 to be allowed")
	}
	if !e.IsChannelAllowed("2222") {
		t.Error("expected channel 2222 to be allowed")
	}
	if e.IsChannelAllowed("9999") {
		t.Error("expected channel 9999 to be rejected")
	}
}

func TestIsChannelAllowed_EmptySetAllowsAll(t *testing.T) {
	baseDir := t.TempDir()
	e := newTestEngineWithMultiWorkspace(t, baseDir)

	// No SeedChannelBindings called — allowedChannels is nil
	if !e.IsChannelAllowed("any-channel") {
		t.Error("expected all channels allowed when no bindings configured")
	}
}

func TestHandleMessage_DropsUnboundChannel(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "sessions.json")
	bindingPath := filepath.Join(tmpDir, "bindings.json")

	p := &stubPlatformWithSend{name: "discord"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, sessionPath, LangEnglish)
	e.SetMultiWorkspace(baseDir, bindingPath)
	e.SeedChannelBindings(map[string]string{
		"1111": "backend",
	})

	// Message from allowed channel — should NOT be dropped (no reply to indicate rejection)
	msg1 := &Message{
		SessionKey: "discord:1111:user1",
		Platform:   "discord",
		UserID:     "user1",
		UserName:   "User",
		Content:    "hello",
		ReplyCtx:   "rctx1",
	}
	e.handleMessage(p, msg1)
	// We don't check exact behavior since agent session startup is complex,
	// just verify no "not allowed" reply was sent

	// Message from disallowed channel — should be silently dropped
	msg2 := &Message{
		SessionKey: "discord:9999:user1",
		Platform:   "discord",
		UserID:     "user1",
		UserName:   "User",
		Content:    "hello from unbound channel",
		ReplyCtx:   "rctx2",
	}
	p.sent = nil // reset
	e.handleMessage(p, msg2)

	// Verify nothing was sent back (silently dropped)
	if len(p.sent) > 0 {
		t.Errorf("expected no reply for unbound channel, got %d messages: %v", len(p.sent), p.sent)
	}
}

// stubPlatformWithSend records sent messages for assertion.
type stubPlatformWithSend struct {
	name string
	sent []string
}

func (p *stubPlatformWithSend) Name() string                                         { return p.name }
func (p *stubPlatformWithSend) Start(MessageHandler) error                           { return nil }
func (p *stubPlatformWithSend) Reply(_ context.Context, _ any, content string) error { p.sent = append(p.sent, content); return nil }
func (p *stubPlatformWithSend) Send(_ context.Context, _ any, content string) error  { p.sent = append(p.sent, content); return nil }
func (p *stubPlatformWithSend) Stop() error                                          { return nil }
