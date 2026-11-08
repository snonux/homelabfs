//go:build linux || freebsd

package runtimeprep

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

const usage = "usage: hfs-runtime-prep generate --spec FILE --output NEW_DIR | " +
	"enroll-request --template FILE --proposal FILE --boot-id UUID --provider-nonce UUID " +
	"--device-names virtioA,virtioB --observer FILE --output NEW_FILE | " +
	"bind-request --template FILE --provision FILE --awaiting FILE --output NEW_FILE"

// Run executes one subcommand. None of them reads the operator private key,
// contacts a host or starts a runtime; every output is a new private file.
func Run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	switch args[0] {
	case "generate":
		return runGenerate(ctx, args[1:], stdout)
	case "enroll-request":
		return runEnrollRequest(args[1:], stdout)
	case "bind-request":
		return runBindRequest(args[1:], stdout)
	}
	return fmt.Errorf("unknown subcommand %q; %s", args[0], usage)
}

func runGenerate(ctx context.Context, args []string, stdout io.Writer) error {
	f := flag.NewFlagSet("generate", flag.ContinueOnError)
	spec := f.String("spec", "", "private closed spec JSON")
	output := f.String("output", "", "fresh absolute output directory")
	if err := parseAll(f, args, spec, output); err != nil {
		return err
	}
	var s Spec
	if _, err := readStrict(*spec, &s); err != nil {
		return err
	}
	bundle, err := Generate(ctx, s, *output, rand.Reader)
	if err != nil {
		return err
	}
	if err := writeFreshDirectory(*output, bundle.Files); err != nil {
		return err
	}
	return printJSON(stdout, bundle.Record)
}

func runEnrollRequest(args []string, stdout io.Writer) error {
	f := flag.NewFlagSet("enroll-request", flag.ContinueOnError)
	template := f.String("template", "", "generated request-template.json")
	proposal := f.String("proposal", "", "private copy of the runtime's actual proposal.json")
	boot := f.String("boot-id", "", "actual measured guest boot UUID")
	nonce := f.String("provider-nonce", "", "actual measured provider nonce UUID")
	devices := f.String("device-names", "", "guest virtio device names of the two domains, in domain order")
	observer := f.String("observer", "", "private observer input JSON")
	output := f.String("output", "", "new private request file")
	if err := parseAll(f, args, template, proposal, boot, nonce, devices, observer, output); err != nil {
		return err
	}
	var t RequestTemplate
	in := EnrollmentInputs{ProposalPath: *proposal, BootID: *boot, ProviderNonce: *nonce,
		DeviceNames: strings.Split(*devices, ",")}
	if _, err := readStrict(*template, &t); err != nil {
		return err
	}
	if _, err := readStrict(*proposal, &in.Proposal); err != nil {
		return err
	}
	if _, err := readStrict(*observer, &in.Observer); err != nil {
		return err
	}
	request, err := FillEnrollment(t, in)
	if err != nil {
		return err
	}
	return writeRequest(*output, request, stdout)
}

func runBindRequest(args []string, stdout io.Writer) error {
	f := flag.NewFlagSet("bind-request", flag.ContinueOnError)
	template := f.String("template", "", "generated request-template.json")
	provision := f.String("provision", "", "signer-emitted provision.json")
	awaiting := f.String("awaiting", "", "the runtime's AWAITING_GUEST_BIND record")
	output := f.String("output", "", "new private request file")
	if err := parseAll(f, args, template, provision, awaiting, output); err != nil {
		return err
	}
	var t RequestTemplate
	if _, err := readStrict(*template, &t); err != nil {
		return err
	}
	provisionRaw, err := readPrivate(*provision, 128<<10) // the signer's provision bound
	if err != nil {
		return err
	}
	awaitingRaw, err := readPrivate(*awaiting, 4096)
	if err != nil {
		return err
	}
	request, err := FillBind(t, *provision, provisionRaw, awaitingRaw)
	if err != nil {
		return err
	}
	return writeRequest(*output, request, stdout)
}

// parseAll parses flags and requires every listed flag to be non-empty.
func parseAll(f *flag.FlagSet, args []string, required ...*string) error {
	f.SetOutput(io.Discard)
	if err := f.Parse(args); err != nil {
		return fmt.Errorf("%w; %s", err, usage)
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments; %s", usage)
	}
	for _, value := range required {
		if *value == "" {
			return fmt.Errorf("missing required flag; %s", usage)
		}
	}
	return nil
}

// writeRequest writes the request exclusively and prints its path and hash.
func writeRequest(path string, request any, stdout io.Writer) error {
	if !cleanAbsolute(path) {
		return fmt.Errorf("require a clean absolute output path")
	}
	raw, err := marshalFile(request)
	if err != nil {
		return err
	}
	if err := writeExclusive(path, raw); err != nil {
		return err
	}
	return printJSON(stdout, map[string]string{"output": path, "sha256": sha256Hex(raw)})
}

func printJSON(w io.Writer, value any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(value)
}
