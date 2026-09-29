// Command soroauth-server exposes soroauth's offline verification over HTTP,
// for wallets and custody systems that want a pre-submission check without
// embedding Go.
//
// It is stateless and holds no keys: verification rebuilds the signing payload
// from the entry itself and checks the signatures against it, which needs
// public data only. There is no field that accepts a secret, nothing is
// stored, and request bodies are never logged. Rate limiting, authentication
// and TLS are the operator's concern — put this behind a reverse proxy that
// provides them — and the documentation states that rather than inventing a
// scheme here.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/soroauth/soroauth-go"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// maxRequestBodySize bounds the JSON request body. An authorization entry
// carries a small invocation tree; the library refuses decoded entries past
// soroauth.MaxDecodeInputBytes, and the base64 form of such an entry plus its
// JSON framing fits comfortably under this cap.
const maxRequestBodySize = 1 << 20 // 1 MB

// verifyRequest is the JSON request body for POST /verify.
type verifyRequest struct {
	// Entry is the base64 authorization entry or transaction envelope.
	Entry string `json:"entry"`
	// Network is testnet, futurenet, public, or a literal network passphrase.
	Network string `json:"network"`
	// ValidUntilLedger is an optional assertion of the expiration the entry
	// carries. The payload is always rebuilt from the entry's own stored
	// expiration; a disagreement is an error, never an override.
	ValidUntilLedger uint32 `json:"valid_until_ledger,omitempty"`
}

// nodeOutput is one credential node's verdict in the JSON report. Its shape is
// deliberately identical to the CLI's verify output, so a client parses one
// schema for both.
type nodeOutput struct {
	Address string `json:"address"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason,omitempty"`
}

// entryOutput is one entry's verification report in the JSON output,
// identical to the CLI's verify output.
type entryOutput struct {
	OperationIndex   int          `json:"operation_index,omitempty"`
	EntryIndex       int          `json:"entry_index,omitempty"`
	CredentialType   string       `json:"credential_type"`
	AddressBound     bool         `json:"address_bound"`
	Address          string       `json:"address,omitempty"`
	ValidUntilLedger uint32       `json:"valid_until_ledger,omitempty"`
	Verified         bool         `json:"verified"`
	Nodes            []nodeOutput `json:"nodes"`
	Note             string       `json:"note,omitempty"`
}

// errorOutput is the failure body. It matches the CLI's --json error object.
type errorOutput struct {
	Error string `json:"error"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("soroauth-server %s\n", Version)
		return
	}

	addr := getEnv("ADDR", ":8080")
	readTimeout := getEnvDuration("READ_TIMEOUT", 5*time.Second)
	writeTimeout := getEnvDuration("WRITE_TIMEOUT", 10*time.Second)
	idleTimeout := getEnvDuration("IDLE_TIMEOUT", 120*time.Second)

	srv := &http.Server{
		Addr:         addr,
		Handler:      newMux(),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	go func() {
		log.Printf("starting server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}
	log.Println("server stopped")
}

// newMux wires the routes. It is a constructor rather than inline in main so
// the integration tests serve the exact same routes over httptest.
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/verify", verifyHandler)
	return mux
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": Version,
	})
}

// verifyHandler answers one question about each entry it is handed: do the
// signatures already on it verify over the payload it commits to?
//
// The verdicts are the CLI's: verified, unsigned, invalid, cannot_check, one
// per credential node, in the CLI's JSON shape — a single object for one
// entry, an array carrying operation_index and entry_index for an envelope.
// A verification that completes is HTTP 200 whatever the verdicts say: the
// verdicts are the answer, and a body that says "invalid" is not a transport
// failure. That maps to the CLI's exit code 4, which likewise fires after the
// report is printed. Only a request the service cannot verify — malformed
// JSON, a missing field, an undecodable entry, an engine error, or a
// valid_until_ledger assertion the entry contradicts — is 4xx with an
// {"error"} body and no report.
func verifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// An oversize body is reported as 413 rather than 400: the request
		// was well-formed but too large, so retrying it unchanged can never
		// succeed and the caller should know why at a glance.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds the 1 MB limit")
			return
		}
		writeError(w, http.StatusBadRequest, "reading request body: "+err.Error())
		return
	}

	var req verifyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "parsing JSON: "+err.Error())
		return
	}
	if req.Entry == "" {
		writeError(w, http.StatusBadRequest, "entry is required")
		return
	}
	passphrase, err := resolveNetwork(req.Network)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	input, err := decodeEntryOrEnvelope(req.Entry)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	type located struct {
		entry          xdr.SorobanAuthorizationEntry
		operationIndex int
		entryIndex     int
	}
	var locatedEntries []located
	if input.isEnvelope {
		entries, err := soroauth.EnvelopeEntries(input.envelope)
		if err != nil {
			writeError(w, http.StatusBadRequest, "envelope has no authorization entries: "+err.Error())
			return
		}
		for _, e := range entries {
			locatedEntries = append(locatedEntries, located{entry: e.Entry, operationIndex: e.OperationIndex, entryIndex: e.EntryIndex})
		}
	} else {
		locatedEntries = append(locatedEntries, located{entry: input.entry})
	}

	outputs := make([]entryOutput, 0, len(locatedEntries))
	for _, item := range locatedEntries {
		report, err := soroauth.VerifyEntry(item.entry, passphrase)
		if err != nil {
			writeError(w, http.StatusBadRequest, "verification failed: "+err.Error())
			return
		}
		if req.ValidUntilLedger != 0 && report.ValidUntilLedger != req.ValidUntilLedger {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("entry carries expiration %d, but valid_until_ledger says %d",
					report.ValidUntilLedger, req.ValidUntilLedger))
			return
		}
		out := newEntryOutput(report)
		if input.isEnvelope {
			out.OperationIndex = item.operationIndex + 1
			out.EntryIndex = item.entryIndex + 1
		}
		outputs = append(outputs, out)
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if input.isEnvelope {
		_ = enc.Encode(outputs)
	} else {
		_ = enc.Encode(outputs[0])
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorOutput{Error: msg})
}

func newEntryOutput(report soroauth.VerificationReport) entryOutput {
	out := entryOutput{
		CredentialType:   report.CredentialType,
		AddressBound:     report.AddressBound,
		Address:          report.Address,
		ValidUntilLedger: report.ValidUntilLedger,
		Verified:         report.Verified(),
		Note:             report.Note,
		Nodes:            make([]nodeOutput, 0, len(report.Nodes)),
	}
	for _, node := range report.Nodes {
		out.Nodes = append(out.Nodes, nodeOutput{
			Address: node.Address,
			Verdict: string(node.Verdict),
			Reason:  node.Reason,
		})
	}
	return out
}

// resolveNetwork turns the network field into a passphrase. The three named
// networks are shorthands resolved from the SDK's constants — the same source
// the CLI uses, so the two cannot drift; anything else is a literal
// passphrase, so a standalone network works without this service knowing
// about it.
func resolveNetwork(value string) (string, error) {
	switch value {
	case "":
		return "", errors.New("network is required (testnet, futurenet, public, or a literal passphrase)")
	case "testnet":
		return network.TestNetworkPassphrase, nil
	case "futurenet":
		return network.FutureNetworkPassphrase, nil
	case "public":
		return network.PublicNetworkPassphrase, nil
	default:
		return value, nil
	}
}

// decodedInput is a base64 blob that was either an authorization entry or a
// transaction envelope, together with which of the two it turned out to be.
type decodedInput struct {
	entry      xdr.SorobanAuthorizationEntry
	envelope   xdr.TransactionEnvelope
	isEnvelope bool
}

// decodeEntryOrEnvelope reads the entry field, which accepts either shape.
//
// Both are base64 XDR unions whose first bytes can overlap, so a blob is
// tried against both and judged on what it decodes to: an envelope only wins
// when it decodes and carries entries to authorize, which an authorization
// entry never looks like. Anything else that fails both readings gets the
// entry error, since the field has always meant an entry first. This mirrors
// the CLI's --entry handling, which is what keeps the verdicts identical.
func decodeEntryOrEnvelope(value string) (decodedInput, error) {
	var envelope xdr.TransactionEnvelope
	envelopeErr := xdr.SafeUnmarshalBase64(value, &envelope)

	var entry xdr.SorobanAuthorizationEntry
	entryErr := xdr.SafeUnmarshalBase64(value, &entry)

	if envelopeErr == nil {
		if entries, err := soroauth.EnvelopeEntries(envelope); err == nil && len(entries) > 0 {
			return decodedInput{envelope: envelope, isEnvelope: true}, nil
		} else if entryErr != nil {
			return decodedInput{}, fmt.Errorf("decoding entry as an envelope: %w", err)
		}
	}

	if entryErr != nil {
		return decodedInput{}, fmt.Errorf("decoding entry: %w", entryErr)
	}
	return decodedInput{entry: entry}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
