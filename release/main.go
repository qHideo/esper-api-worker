// Command release generates update signing keys and signs release manifests.
//
// The manifest wire format matches the launcher's verifier:
//
//	{"schema":1,"channel":"beta","version":"0.1.32","build_number":32,
//	 "expires":"...","notes_url":"...","artifacts":{"windows-x64":{...}}}
//
// wrapped as {"key_id","signature","manifest"} where manifest is base64 of the
// exact JSON bytes and signature is base64 Ed25519 over those bytes.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const schema = 1

type artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type manifest struct {
	Schema      int                 `json:"schema"`
	Channel     string              `json:"channel"`
	Version     string              `json:"version"`
	BuildNumber uint64              `json:"build_number"`
	Expires     string              `json:"expires"`
	NotesURL    string              `json:"notes_url,omitempty"`
	Artifacts   map[string]artifact `json:"artifacts"`
}

type envelope struct {
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
	Manifest  string `json:"manifest"`
}

func main() {
	if len(os.Args) < 2 {
		fail("usage: release keygen|pubkey|sign|verify [flags]")
	}
	switch os.Args[1] {
	case "keygen":
		keygen(os.Args[2:])
	case "pubkey":
		pubkey(os.Args[2:])
	case "sign":
		sign(os.Args[2:])
	case "verify":
		verify(os.Args[2:])
	default:
		fail("unknown command %q", os.Args[1])
	}
}

func keygen(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	dir := fs.String("out", "keys", "directory that receives seed.b64")
	fs.Parse(args)

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fail("generate key: %v", err)
	}
	if err := os.MkdirAll(*dir, 0o700); err != nil {
		fail("create key directory: %v", err)
	}
	seedPath := filepath.Join(*dir, "seed.b64")
	encoded := base64.StdEncoding.EncodeToString(private.Seed()) + "\n"
	if err := os.WriteFile(seedPath, []byte(encoded), 0o600); err != nil {
		fail("write seed: %v", err)
	}

	fmt.Printf("seed written to %s (keep it secret)\n", seedPath)
	fmt.Printf("public key: %s\n", base64.StdEncoding.EncodeToString(public))
	fmt.Printf("launcher constant: const UPDATE_PUBLIC_KEY: &str = %q;\n",
		base64.StdEncoding.EncodeToString(public))
}

func pubkey(args []string) {
	fs := flag.NewFlagSet("pubkey", flag.ExitOnError)
	seedPath := fs.String("seed-file", "keys/seed.b64", "base64 Ed25519 seed")
	fs.Parse(args)

	private := readSeed(*seedPath)
	fmt.Println(base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey)))
}

func sign(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	artifactPath := fs.String("artifact", "", "release archive to hash and sign")
	url := fs.String("url", "", "HTTPS download URL for the artifact")
	version := fs.String("version", "", "release version, e.g. 0.1.32")
	build := fs.Uint64("build", 0, "monotonic publication number")
	channel := fs.String("channel", "beta", "beta, nightly or stable")
	expires := fs.Duration("expires", 90*24*time.Hour, "manifest lifetime")
	keyID := fs.String("key-id", "esper-2026", "signing key identifier")
	seedPath := fs.String("seed-file", "keys/seed.b64", "base64 Ed25519 seed")
	platform := fs.String("platform", "windows-x64", "artifact platform key")
	notesURL := fs.String("notes-url", "", "release notes URL")
	out := fs.String("out", "public/v1/latest.json", "envelope output path")
	fs.Parse(args)

	if *artifactPath == "" || *url == "" || *version == "" {
		fail("sign requires -artifact, -url and -version")
	}
	if *build == 0 {
		fail("-build must be a positive publication number")
	}
	if len(*url) < 8 || (*url)[:8] != "https://" {
		fail("-url must be HTTPS")
	}

	payload, err := os.ReadFile(*artifactPath)
	if err != nil {
		fail("read artifact: %v", err)
	}
	digest := sha256.Sum256(payload)
	entry := artifact{
		URL:    *url,
		SHA256: hex.EncodeToString(digest[:]),
		Size:   int64(len(payload)),
	}

	doc := manifest{
		Schema:      schema,
		Channel:     *channel,
		Version:     *version,
		BuildNumber: *build,
		Expires:     time.Now().UTC().Add(*expires).Format(time.RFC3339),
		NotesURL:    *notesURL,
		Artifacts:   map[string]artifact{*platform: entry},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		fail("encode manifest: %v", err)
	}

	private := readSeed(*seedPath)
	signature := ed25519.Sign(private, body)
	encoded, err := json.MarshalIndent(envelope{
		KeyID:     *keyID,
		Signature: base64.StdEncoding.EncodeToString(signature),
		Manifest:  base64.StdEncoding.EncodeToString(body),
	}, "", "  ")
	if err != nil {
		fail("encode envelope: %v", err)
	}
	encoded = append(encoded, '\n')
	if dir := filepath.Dir(*out); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail("create output directory: %v", err)
		}
	}
	if err := os.WriteFile(*out, encoded, 0o644); err != nil {
		fail("write envelope: %v", err)
	}

	fmt.Printf("signed %s %s build %d (%d bytes, sha256 %s)\n",
		*channel, *version, *build, entry.Size, entry.SHA256[:16])
	fmt.Printf("wrote %s\n", *out)
}

func verify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	file := fs.String("file", "public/v1/latest.json", "envelope to verify")
	seedPath := fs.String("seed-file", "keys/seed.b64", "base64 Ed25519 seed")
	fs.Parse(args)

	raw, err := os.ReadFile(*file)
	if err != nil {
		fail("read envelope: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		fail("decode envelope: %v", err)
	}
	body, err := base64.StdEncoding.DecodeString(env.Manifest)
	if err != nil {
		fail("decode manifest: %v", err)
	}
	signature, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		fail("decode signature: %v", err)
	}
	private := readSeed(*seedPath)
	if !ed25519.Verify(private.Public().(ed25519.PublicKey), body, signature) {
		fail("signature is invalid")
	}
	var doc manifest
	if err := json.Unmarshal(body, &doc); err != nil {
		fail("parse manifest: %v", err)
	}
	if doc.Schema != schema {
		fail("unsupported schema %d", doc.Schema)
	}
	if _, err := time.Parse(time.RFC3339, doc.Expires); err != nil {
		fail("invalid expiry: %v", err)
	}
	fmt.Printf("valid: %s %s build %d, expires %s, key %s\n",
		doc.Channel, doc.Version, doc.BuildNumber, doc.Expires, env.KeyID)
}

func readSeed(path string) ed25519.PrivateKey {
	raw, err := os.ReadFile(path)
	if err != nil {
		fail("read seed: %v", err)
	}
	text := string(raw)
	for len(text) > 0 && (text[len(text)-1] == '\n' || text[len(text)-1] == '\r') {
		text = text[:len(text)-1]
	}
	seed, err := base64.StdEncoding.DecodeString(text)
	if err != nil || len(seed) != ed25519.SeedSize {
		fail("seed file must contain base64 of %d bytes", ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "release: "+format+"\n", args...)
	os.Exit(1)
}
