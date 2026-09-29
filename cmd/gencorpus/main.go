// Command gencorpus builds the fuzz seed corpora for all fuzz targets
// from the committed golden vectors.
//
// Why generate it rather than hand-write seeds: the golden vectors are the
// entries this library is proven against, so they are exactly the inputs a
// fuzzer should start from — every credential arm, a sub-invocation tree, a
// create-contract invocation, the int64 nonce edges, and three delegate shapes
// including one address at two nesting depths. Starting the fuzzer from real
// entries means its mutations begin inside the space of things that decode,
// instead of spending the budget discovering what a valid entry looks like.
//
// Three details about the output are not free choices:
//
//   - The directory is testdata/fuzz/<TargetName>. Go reads a
//     target's seed corpus from testdata/fuzz/<TargetName>, and only from
//     there; files anywhere else, including directly in testdata/fuzz, are
//     ignored.
//   - The file format is Go's corpus format — the "go test fuzz v1" header
//     followed by one Go literal per fuzz argument — not the raw bytes. A file
//     in that directory that is not in this format fails the package's tests
//     rather than being skipped.
//   - Cleanup is manifest-tracked, not RemoveAll. A fuzzer that finds a crash
//     writes the reproducer into testdata/fuzz/<TargetName>, and that file is
//     committed as a regression seed — so gencorpus must never delete a file
//     it did not write. Each target's generated filenames are recorded in
//     testdata/fuzz/.gencorpus/<TargetName>, and only those files are removed
//     before rewriting. Anything else in the seed directory is left alone.
//
// The seed is the decoded entry, not the base64 text, because the target's
// argument is the []byte it hands to UnmarshalBinary.
//
// Run from the repository root:
//
//	go run ./cmd/gencorpus
//
// It is deterministic: the same vectors produce byte-identical corpus files, so
// CI regenerates and fails on drift the same way it does for the vectors.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	vectorsDir = "testdata/vectors"
	fuzzDir    = "testdata/fuzz"
	// manifestDir records, per fuzz target, the seed filenames gencorpus
	// generated. It lives outside the seed directories because Go treats
	// every file inside testdata/fuzz/<TargetName> as a seed: a manifest
	// there would fail the package's tests instead of being skipped.
	manifestDir = "testdata/fuzz/.gencorpus"
)

// vector is the part of a golden vector this reads: the entry before any
// delegate wrapping, and the entry as it goes to the signer. Both are entries
// the fuzz target can decode, and pre_wrap_entry_xdr is absent from most
// vectors.
type vector struct {
	PreWrapEntryXDR  string `json:"pre_wrap_entry_xdr"`
	UnsignedEntryXDR string `json:"unsigned_entry_xdr"`
}

// targets is the list of fuzz targets that use entry bytes as input.
var targets = []string{
	"FuzzValidateDelegateOrder",
	"FuzzInspect",
	"FuzzPreimage",
	"FuzzDecodeAuthorizationEntry",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gencorpus: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	entries, err := os.ReadDir(vectorsDir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", vectorsDir, err)
	}

	// Sorted, so the output does not depend on directory order.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("no vectors found in %s; run this from the repository root", vectorsDir)
	}

	totalWritten := 0
	for _, target := range targets {
		outDir := filepath.Join(fuzzDir, target)

		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", outDir, err)
		}
		// Remove only what the previous run generated — a stale seed whose
		// vector was deleted or renamed. Anything else in the directory is
		// a committed crash reproducer and is left alone.
		generated := manifestNames(filepath.Join(manifestDir, target))
		for _, name := range generated {
			if err := os.Remove(filepath.Join(outDir, name)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("clearing %s: %w", filepath.Join(outDir, name), err)
			}
		}

		written := 0
		var writtenNames []string
		for _, name := range names {
			path := filepath.Join(vectorsDir, name)
			raw, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}

			var v vector
			if err := json.Unmarshal(raw, &v); err != nil {
				return fmt.Errorf("parsing %s: %w", path, err)
			}

			// A malformed vector is an error, not something to skip: a silently
			// skipped vector is a seed the fuzzer never gets, and nothing would
			// say so.
			stem := name[:len(name)-len(".json")]
			for i, encoded := range []string{v.UnsignedEntryXDR, v.PreWrapEntryXDR} {
				if encoded == "" {
					continue
				}
				decoded, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					return fmt.Errorf("decoding entry %d of %s: %w", i, path, err)
				}
				seedName := fmt.Sprintf("%s_%d", stem, i)
				outPath := filepath.Join(outDir, seedName)
				if err := os.WriteFile(outPath, corpusFile(decoded), 0o644); err != nil {
					return fmt.Errorf("writing %s: %w", outPath, err)
				}
				writtenNames = append(writtenNames, seedName)
				written++
			}
		}
		if err := writeManifest(target, writtenNames); err != nil {
			return fmt.Errorf("writing the %s manifest: %w", target, err)
		}

		fmt.Printf("wrote %d seeds for %s into %s\n", written, target, outDir)
		totalWritten += written
	}

	// FuzzPayload takes []byte as input but doesn't use entry XDR.
	// We add some basic payload seeds separately.
	if err := writePayloadSeeds(); err != nil {
		return fmt.Errorf("writing payload seeds: %w", err)
	}

	fmt.Printf("total seeds written: %d\n", totalWritten)
	return nil
}

// corpusFile renders one seed in Go's fuzz corpus format: the version header,
// then one Go literal per argument of the fuzz function. All targets take
// a single []byte, so there is one literal.
func corpusFile(data []byte) []byte {
	return []byte("go test fuzz v1\n[]byte(" + strconv.Quote(string(data)) + ")\n")
}

func writePayloadSeeds() error {
	target := "FuzzPayload"
	outDir := filepath.Join(fuzzDir, target)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}
	generated := manifestNames(filepath.Join(manifestDir, target))
	for _, name := range generated {
		if err := os.Remove(filepath.Join(outDir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clearing %s: %w", filepath.Join(outDir, name), err)
		}
	}

	seeds := [][]byte{
		{0},
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
		{0x01, 0x02, 0x03, 0x04},
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	}

	var writtenNames []string
	for i, seed := range seeds {
		seedName := fmt.Sprintf("seed_%d", i)
		outPath := filepath.Join(outDir, seedName)
		if err := os.WriteFile(outPath, corpusFile(seed), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", outPath, err)
		}
		writtenNames = append(writtenNames, seedName)
	}
	if err := writeManifest(target, writtenNames); err != nil {
		return fmt.Errorf("writing the %s manifest: %w", target, err)
	}

	fmt.Printf("wrote %d seeds for %s into %s\n", len(seeds), target, outDir)
	return nil
}

// manifestNames reads the filenames a previous run recorded for a target.
// A missing manifest is not an error: it means nothing was generated yet,
// e.g. the first run after this tracking was added.
func manifestNames(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}

// writeManifest records the seed filenames a run generated, sorted so the
// file is deterministic. The next run deletes exactly these files before
// rewriting, which is what lets a stale seed disappear without touching a
// committed crash reproducer sharing the directory.
func writeManifest(target string, names []string) error {
	sort.Strings(names)
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", manifestDir, err)
	}
	body := ""
	if len(names) > 0 {
		body = strings.Join(names, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(manifestDir, target), []byte(body), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Join(manifestDir, target), err)
	}
	return nil
}
