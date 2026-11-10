package ai

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- helpers ---------------------------------------------------------------

func pinForTest(t *testing.T, rawURL string, body []byte) {
	t.Helper()
	sum := sha256.Sum256(body)
	old, had := pinnedArtifacts[rawURL]
	pinnedArtifacts[rawURL] = artifactPin{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}
	t.Cleanup(func() {
		if had {
			pinnedArtifacts[rawURL] = old
		} else {
			delete(pinnedArtifacts, rawURL)
		}
	})
}

func allowDownloadHostForTest(t *testing.T, base string) {
	t.Helper()
	bu, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	old := isAllowedDownloadURL
	isAllowedDownloadURL = func(u *url.URL) bool {
		return old(u) || (u != nil && u.Host == bu.Host && u.Scheme == bu.Scheme)
	}
	t.Cleanup(func() { isAllowedDownloadURL = old })
}

type tarEnt struct {
	name string
	typ  byte
	mode int64
	body string
	link string
}

func makeTarBz2(t *testing.T, ents []tarEnt) []byte {
	t.Helper()
	if _, err := os.Stat("/usr/bin/bzip2"); err != nil {
		t.Skip("bzip2 not available to build fixtures")
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range ents {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: e.mode, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/usr/bin/bzip2", "-c")
	cmd.Stdin = &buf
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// fakeTransport serves fixed bodies by URL and records every request. It
// never touches the network.
type fakeTransport struct {
	mu       sync.Mutex
	bodies   map[string][]byte
	redirect map[string]string
	urls     []string
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.urls = append(f.urls, r.URL.String())
	f.mu.Unlock()
	if loc, ok := f.redirect[r.URL.String()]; ok {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {loc}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}
	body, ok := f.bodies[r.URL.String()]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}

func (f *fakeTransport) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.urls...)
}

// isolatePath hides any sherpa-onnx/piper/say binaries on the developer's PATH.
func isolatePath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func listTree(root string) []string {
	var out []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && p != root {
			rel, _ := filepath.Rel(root, p)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

func requireUnixRuntimePlatform(t *testing.T) (archiveURL, topDir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script runtime fixture is POSIX only")
	}
	u, top, ok := sherpaRuntimeArchiveURL()
	if !ok {
		t.Skip("no runtime archive for this platform")
	}
	return u, top
}

// --- AI-1: pins, digests, redirects ---------------------------------------

func TestPinnedArtifactsCoverEveryDownload(t *testing.T) {
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, spec := range voiceModelCatalog {
		pin, ok := pinnedArtifacts[spec.BundleURL]
		if !ok || !hex64.MatchString(pin.SHA256) || pin.Size <= 0 {
			t.Errorf("voice bundle %s has no valid pin", spec.BundleURL)
		}
	}
	for _, p := range [][2]string{{"darwin", "arm64"}, {"darwin", "amd64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}} {
		u, _, ok := sherpaRuntimeArchiveURLFor(p[0], p[1])
		if !ok {
			t.Fatalf("no runtime URL for %v", p)
		}
		pin, ok := pinnedArtifacts[u]
		if !ok || !hex64.MatchString(pin.SHA256) || pin.Size <= 0 {
			t.Errorf("runtime %s has no valid pin", u)
		}
	}
	for u := range pinnedArtifacts {
		pu, err := url.Parse(u)
		if err != nil || !isAllowedDownloadURL(pu) {
			t.Errorf("pinned URL %s is not on the download allow-list", u)
		}
	}
}

func TestDownloadRefusesUnpinnedAsset(t *testing.T) {
	ft := &fakeTransport{bodies: map[string][]byte{"https://github.com/x/y/releases/download/t/a.bin": []byte("payload")}}
	dest := filepath.Join(t.TempDir(), "a.bin")
	err := downloadFileAtomic(context.Background(), &http.Client{Transport: ft}, "https://github.com/x/y/releases/download/t/a.bin", dest, nil)
	if err == nil {
		t.Fatal("expected unpinned download to be refused")
	}
	if len(ft.requests()) != 0 {
		t.Fatalf("unpinned asset was requested: %v", ft.requests())
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("file written for unpinned asset")
	}
}

// The reviewer's PoC: a tampered runtime archive on the real release URL is
// refused before extraction and nothing executable is left behind.
func TestDownloadVoiceModelRefusesTamperedRuntime(t *testing.T) {
	archiveURL, top := requireUnixRuntimePlatform(t)
	isolatePath(t)
	evil := makeTarBz2(t, []tarEnt{{name: top + "/bin/sherpa-onnx-offline-tts", mode: 0o755, body: "#!/bin/sh\n/usr/bin/touch /tmp/pwned\n"}})
	ft := &fakeTransport{bodies: map[string][]byte{archiveURL: evil}}
	models := filepath.Join(t.TempDir(), "models")

	err := DownloadVoiceModelToDir(context.Background(), &http.Client{Transport: ft}, models, "en_US-lessac-medium", nil)
	if err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("expected integrity failure, got %v", err)
	}
	if left := listTree(models); len(left) != 0 {
		t.Fatalf("files left behind after refused install: %v", left)
	}
	if bin := resolveSherpaBinaryInDir(models); bin != "" {
		t.Fatalf("runtime resolved after refused install: %s", bin)
	}
}

func TestDownloadRefusesTamperedVoiceBundle(t *testing.T) {
	spec := voiceModelCatalog[0]
	ft := &fakeTransport{bodies: map[string][]byte{spec.BundleURL: []byte("not the pinned bytes")}}
	dest := filepath.Join(t.TempDir(), "voice")
	err := downloadAndExtractTarBz2(context.Background(), &http.Client{Transport: ft}, spec.BundleURL, dest, extractPolicy{stripPrefix: spec.BundleDir}, nil)
	if err == nil {
		t.Fatal("expected digest mismatch")
	}
	if left := listTree(filepath.Dir(dest)); len(left) != 0 {
		t.Fatalf("files left behind: %v", left)
	}
}

func TestDownloadRedirectAllowList(t *testing.T) {
	src := "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/test-redirect.tar.bz2"
	pinForTest(t, src, []byte("x"))
	for _, target := range []string{
		"https://evil.example/payload",
		"http://objects.githubusercontent.com/payload", // https -> http downgrade
		"https://127.0.0.1/payload",
	} {
		ft := &fakeTransport{redirect: map[string]string{src: target}, bodies: map[string][]byte{target: []byte("x")}}
		dest := filepath.Join(t.TempDir(), "out")
		if err := downloadFileAtomic(context.Background(), &http.Client{Transport: ft}, src, dest, nil); err == nil {
			t.Errorf("redirect to %s was followed", target)
		}
		for _, r := range ft.requests() {
			if r == target {
				t.Errorf("request was sent to disallowed redirect target %s", target)
			}
		}
	}
	// Allowed hop: github.com -> release-assets.githubusercontent.com.
	target := "https://release-assets.githubusercontent.com/asset"
	ft := &fakeTransport{redirect: map[string]string{src: target}, bodies: map[string][]byte{target: []byte("x")}}
	if err := downloadFileAtomic(context.Background(), &http.Client{Transport: ft}, src, filepath.Join(t.TempDir(), "ok"), nil); err != nil {
		t.Fatalf("allowed redirect failed: %v", err)
	}
	// Non-https initial URL is refused even when pinned.
	plain := "http://github.com/k2-fsa/x.tar.bz2"
	pinForTest(t, plain, []byte("x"))
	if err := downloadFileAtomic(context.Background(), &http.Client{Transport: &fakeTransport{}}, plain, filepath.Join(t.TempDir(), "p"), nil); err == nil {
		t.Fatal("plain http download was allowed")
	}
}

// --- AI-1 manifest + AI-5 extraction + AI-3 argv + AI-9 cmd.Dir ------------

// installFakeRuntimeAndVoice runs the full install with a fake transport and
// test pins for fixture archives on the real URLs.
func installFakeRuntimeAndVoice(t *testing.T, script string) (models string) {
	t.Helper()
	archiveURL, top := requireUnixRuntimePlatform(t)
	isolatePath(t)
	spec := voiceModelCatalog[0]
	runtimeArchive := makeTarBz2(t, []tarEnt{
		{name: top + "/bin/sherpa-onnx-offline-tts", mode: 0o644, body: script},
		{name: top + "/bin/other-tool", mode: 0o755, body: "x"},
		{name: top + "/lib/libevil.dylib", mode: 0o4755, body: "lib"},
		{name: top + "/lib/sym", typ: tar.TypeSymlink, link: "/etc/passwd"},
		{name: top + "/lib/hard", typ: tar.TypeLink, link: top + "/bin/sherpa-onnx-offline-tts"},
		{name: top + "/lib/../../escape.txt", body: "x"},
		{name: "lib/noprefix.txt", body: "x"},
	})
	voiceArchive := makeTarBz2(t, []tarEnt{
		{name: spec.BundleDir + "/" + spec.ONNXFile, body: "m"},
		{name: spec.BundleDir + "/tokens.txt", body: "t"},
		{name: "/abs.txt", body: "x"},
		{name: "../../escape2.txt", body: "x"},
		{name: spec.BundleDir + "/suid", mode: 0o6777, body: "x"},
		{name: spec.BundleDir + "/bin/tool", mode: 0o755, body: "x"},
		{name: spec.BundleDir + "/sym", typ: tar.TypeSymlink, link: "/etc"},
	})
	pinForTest(t, archiveURL, runtimeArchive)
	pinForTest(t, spec.BundleURL, voiceArchive)
	ft := &fakeTransport{bodies: map[string][]byte{archiveURL: runtimeArchive, spec.BundleURL: voiceArchive}}
	models = filepath.Join(t.TempDir(), "cache", "models")
	if err := DownloadVoiceModelToDir(context.Background(), &http.Client{Transport: ft}, models, spec.ID, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	return models
}

func TestExtractionModesLinksAndPrefix(t *testing.T) {
	models := installFakeRuntimeAndVoice(t, "#!/bin/sh\nexit 0\n")
	parent := filepath.Dir(models)
	for _, p := range []string{"escape.txt", "escape2.txt", "abs.txt"} {
		if _, err := os.Stat(filepath.Join(parent, p)); err == nil {
			t.Errorf("%s escaped the extraction dir", p)
		}
	}
	rt := filepath.Join(models, "_runtime")
	exe := filepath.Join(rt, "bin", "sherpa-onnx-offline-tts")
	if fi, err := os.Stat(exe); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("runtime executable mode = %v, err %v; want 0700", fi.Mode().Perm(), err)
	}
	if fi, err := os.Stat(filepath.Join(rt, "lib", "libevil.dylib")); err != nil || fi.Mode() != 0o600 {
		t.Fatalf("library mode = %v (err %v); want plain 0600 without setuid", fi.Mode(), err)
	}
	for _, p := range []string{"bin/other-tool", "lib/sym", "lib/hard", "lib/noprefix.txt", "noprefix.txt"} {
		if _, err := os.Lstat(filepath.Join(rt, p)); err == nil {
			t.Errorf("unexpected runtime entry %s", p)
		}
	}
	voice := filepath.Join(models, sanitizeModelID(voiceModelCatalog[0].ID))
	_ = filepath.Walk(voice, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("symlink created in voice bundle: %s", p)
		}
		if info.Mode().IsRegular() && info.Mode() != 0o600 {
			t.Errorf("voice file %s mode %v; want 0600 (never executable)", p, info.Mode())
		}
		return nil
	})
	if _, err := os.Stat(filepath.Join(rt, runtimeManifestName)); err != nil {
		t.Fatalf("runtime manifest missing: %v", err)
	}
	if err := verifyBundledRuntime(models); err != nil {
		t.Fatalf("fresh runtime fails verification: %v", err)
	}
}

func TestSpeakVerifiesRuntimeAndNeverPassesOptionText(t *testing.T) {
	base := t.TempDir()
	marker := filepath.Join(base, "RAN")
	argvOut := filepath.Join(base, "argv.txt")
	script := fmt.Sprintf("#!/bin/sh\n/usr/bin/touch %q\necho \"PWD:$(pwd)\" >> %q\nfor a in \"$@\"; do echo \"ARG:$a\" >> %q; done\nexit 0\n", marker, argvOut, argvOut)
	models := installFakeRuntimeAndVoice(t, script)
	exe := filepath.Join(models, "_runtime", "bin", "sherpa-onnx-offline-tts")

	eng := &LocalTTSEngine{Preferred: "auto", ModelsDir: models, ModelID: voiceModelCatalog[0].ID, MinSilentWait: 5 * time.Millisecond}
	if eng.Name() != "piper" {
		t.Fatalf("engine = %q, want piper", eng.Name())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = eng.Speak(ctx, "--output-filename=/tmp/should-not-matter hello", "late-night-chill")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("verified runtime did not run: %v", err)
	}
	out, _ := os.ReadFile(argvOut)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	last := strings.TrimPrefix(lines[len(lines)-1], "ARG:")
	if strings.HasPrefix(last, "-") || !strings.Contains(last, "hello") {
		t.Fatalf("spoken text argv element = %q; must not start with '-'", last)
	}
	wantDir, _ := filepath.EvalSymlinks(filepath.Dir(exe))
	gotDir := ""
	for _, l := range lines {
		if strings.HasPrefix(l, "PWD:") {
			gotDir, _ = filepath.EvalSymlinks(strings.TrimPrefix(l, "PWD:"))
		}
	}
	if gotDir != wantDir {
		t.Fatalf("runtime ran in %q, want its own directory %q", gotDir, wantDir)
	}

	// Swap the binary: it must not be executed again.
	_ = os.Remove(marker)
	if err := os.WriteFile(exe, []byte(script+"# tampered\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := verifiedSherpaBinary(models); err == nil {
		t.Fatal("tampered runtime passed verification")
	}
	_ = eng.Speak(ctx, "hello again", "late-night-chill")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("tampered runtime binary was executed")
	}
}

func TestRuntimeWithoutManifestIsNotUsed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	isolatePath(t)
	models := t.TempDir()
	exe := bundledSherpaPath(models)
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if bin := resolveSherpaBinaryInDir(models); bin != "" {
		t.Fatalf("runtime without manifest resolved: %s", bin)
	}
}

func TestRelativeModelsDirRefused(t *testing.T) {
	isolatePath(t)
	dir := t.TempDir()
	t.Chdir(dir)
	exe := bundledSherpaPath(".halpradio/models")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if bin := resolveSherpaBinaryInDir(".halpradio/models"); bin != "" {
		t.Fatalf("relative models dir resolved a runtime: %s", bin)
	}
	if err := DownloadVoiceModelToDir(context.Background(), &http.Client{Transport: &fakeTransport{}}, ".halpradio/models", "en_US-lessac-medium", nil); err == nil {
		t.Fatal("relative models dir accepted for download")
	}
}

// --- AI-5 caps ---------------------------------------------------------------

func TestExtractionCaps(t *testing.T) {
	const src = "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/caps-test.tar.bz2"
	big := strings.Repeat("A", 2048)
	many := []tarEnt{}
	for i := 0; i < 20; i++ {
		many = append(many, tarEnt{name: fmt.Sprintf("top/f%d", i), body: "x"})
	}
	cases := []struct {
		name   string
		ents   []tarEnt
		policy extractPolicy
	}{
		{"per-file cap errors instead of truncating", []tarEnt{{name: "top/a", body: big}}, extractPolicy{stripPrefix: "top", maxFile: 1024}},
		{"cumulative cap", []tarEnt{{name: "top/a", body: big}, {name: "top/b", body: big}}, extractPolicy{stripPrefix: "top", maxFile: 4096, maxTotal: 3000}},
		{"entry cap counts every header", many, extractPolicy{stripPrefix: "top", maxEntries: 10}},
		{"required file missing", []tarEnt{{name: "top/a", body: "x"}}, extractPolicy{stripPrefix: "top", required: []string{"tokens.txt"}}},
		{"top-level prefix required", []tarEnt{{name: "a", body: "x"}}, extractPolicy{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archive := makeTarBz2(t, tc.ents)
			pinForTest(t, src, archive)
			ft := &fakeTransport{bodies: map[string][]byte{src: archive}}
			root := t.TempDir()
			dest := filepath.Join(root, "out")
			if err := downloadAndExtractTarBz2(context.Background(), &http.Client{Transport: ft}, src, dest, tc.policy, nil); err == nil {
				t.Fatal("expected extraction to fail")
			}
			if left := listTree(root); len(left) != 0 {
				t.Fatalf("files left behind: %v", left)
			}
		})
	}
}

// --- AI-9 ----------------------------------------------------------------------

func TestWindowsRuntimePolicyKeepsDLLsNextToExe(t *testing.T) {
	p := runtimeExtractPolicy("windows", "top")
	if !p.keep("bin/sherpa-onnx-offline-tts.exe") || !p.keep("bin/onnxruntime.dll") || !p.keep("lib/sherpa-onnx-c-api.dll") {
		t.Fatal("windows policy drops the exe or its DLLs")
	}
	if p.keep("bin/sherpa-onnx-offline-asr.exe") {
		t.Fatal("windows policy keeps unrelated executables")
	}
	if got := p.mapPath("lib/sherpa-onnx-c-api.dll"); got != "bin/sherpa-onnx-c-api.dll" {
		t.Fatalf("lib DLL mapped to %q, want bin/", got)
	}
	if p.executable("bin/onnxruntime.dll") || !p.executable("bin/sherpa-onnx-offline-tts.exe") {
		t.Fatal("exec flag must be set on the exe only")
	}
	u := runtimeExtractPolicy("linux", "top")
	if u.executable("lib/libonnxruntime.so") || !u.executable("bin/sherpa-onnx-offline-tts") {
		t.Fatal("linux exec flag must be set on the exe only")
	}
}

// --- AI-3 ----------------------------------------------------------------------

func TestSanitizeSpokenTextStripsLeadingDashes(t *testing.T) {
	for _, in := range []string{
		"--output-filename=/tmp/x hello",
		"-h hello",
		"—– --help hello",
		"\x1b[2J--sid=9 hello",
		"​--x hello",
	} {
		got := sanitizeSpokenText(in)
		if strings.HasPrefix(got, "-") || strings.ContainsAny(got, "\x1b​") || !strings.Contains(got, "hello") {
			t.Errorf("sanitizeSpokenText(%q) = %q", in, got)
		}
	}
	if n := len([]rune(sanitizeSpokenText(strings.Repeat("word ", 1000)))); n > maxSpokenRunes {
		t.Errorf("spoken text not capped: %d runes", n)
	}
}
