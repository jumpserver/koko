package assetexec

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anmitsu/go-shlex"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/validation"
)

var kubernetesResource = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./,_:-]*$`)
var kubernetesArgument = regexp.MustCompile(`^[a-zA-Z0-9_./,:=@%+-]+$`)

// PrepareKubernetesCommand fixes the exact argv reviewed by Core. Neither
// quoting, kubectl aliases nor a local shell may change the approved verb.
func PrepareKubernetesCommand(command string) (string, error) {
	args, err := kubernetesArguments(command)
	if err != nil {
		return "", err
	}
	args = append([]string{"kubectl"}, args...)
	for i, value := range args {
		if !kubernetesArgument.MatchString(value) {
			args[i] = shellQuote(value)
		}
	}
	result := strings.Join(args, " ")
	if len(result) > 4096 {
		return "", errors.New("normalized Kubernetes command exceeds 4096 bytes")
	}
	return result, nil
}

func kubernetesArguments(command string) ([]string, error) {
	if len(command) > 4096 || !utf8.ValidString(command) || strings.ContainsRune(command, 0) {
		return nil, errors.New("Kubernetes command must contain valid UTF-8 without NUL, at most 4096 bytes")
	}
	args, err := shlex.Split(command, true)
	if err != nil || len(args) < 3 || args[0] != "kubectl" {
		return nil, errors.New("use one kubectl get, describe or logs command")
	}
	verb := args[1]
	flags := pflag.NewFlagSet("kubectl", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	namespace := flags.StringP("namespace", "n", "", "")
	selector := flags.StringP("selector", "l", "", "")
	switch verb {
	case "get":
		flags.BoolP("all-namespaces", "A", false, "")
		flags.String("field-selector", "", "")
		flags.StringP("output", "o", "", "")
		flags.Bool("show-labels", false, "")
		flags.Bool("no-headers", false, "")
		flags.Bool("ignore-not-found", false, "")
		flags.String("sort-by", "", "")
		flags.Int("chunk-size", 100, "")
	case "describe":
		flags.BoolP("all-namespaces", "A", false, "")
		flags.Bool("show-events", true, "")
	case "logs":
		flags.StringP("container", "c", "", "")
		flags.Bool("all-containers", false, "")
		flags.BoolP("previous", "p", false, "")
		flags.Bool("timestamps", false, "")
		flags.Bool("prefix", false, "")
		flags.String("since", "", "")
		flags.String("since-time", "", "")
		flags.Int("tail", 200, "")
		flags.Int("limit-bytes", 256<<10, "")
	default:
		return nil, errors.New("Kubernetes MCP supports only get, describe and finite logs; use open_terminal for other commands")
	}
	if err := flags.Parse(args[2:]); err != nil {
		return nil, fmt.Errorf("unsupported Kubernetes arguments: %w", err)
	}
	if flags.Changed("namespace") && len(validation.IsDNS1123Label(*namespace)) != 0 {
		return nil, errors.New("namespace must be a valid Kubernetes namespace name")
	}
	if flags.Changed("namespace") && flags.Lookup("all-namespaces") != nil && flags.Lookup("all-namespaces").Value.String() == "true" {
		return nil, errors.New("choose namespace or all-namespaces, not both")
	}
	resources := flags.Args()
	if (len(resources) == 0 && !(verb == "logs" && *selector != "")) || (verb == "logs" && (len(resources) > 1 || len(resources) != 0 && flags.Changed("selector"))) {
		return nil, errors.New("provide resource names, or a logs selector")
	}
	for _, resource := range resources {
		if !kubernetesResource.MatchString(resource) || strings.Count(resource, "/") > 1 {
			return nil, errors.New("resource arguments cannot contain shell expressions, flags or local paths")
		}
	}
	if verb == "get" {
		output, _ := flags.GetString("output")
		if output != "" && output != "json" && output != "yaml" && output != "wide" && output != "name" && !strings.HasPrefix(output, "jsonpath=") && !strings.HasPrefix(output, "custom-columns=") {
			return nil, errors.New("output supports json, yaml, wide, name, or inline jsonpath/custom-columns; file templates are unavailable")
		}
		chunk, _ := flags.GetInt("chunk-size")
		if chunk < 1 || chunk > 500 {
			return nil, errors.New("chunk-size must be between 1 and 500")
		}
		_ = flags.Set("chunk-size", fmt.Sprint(chunk))
	}
	if verb == "logs" {
		tail, _ := flags.GetInt("tail")
		limit, _ := flags.GetInt("limit-bytes")
		if tail < 1 || tail > 10000 || limit < 1 || limit > 1<<20 {
			return nil, errors.New("logs require tail between 1 and 10000 and limit-bytes between 1 and 1048576")
		}
		if flags.Changed("since") {
			value, _ := flags.GetString("since")
			if duration, err := time.ParseDuration(value); err != nil || duration <= 0 {
				return nil, errors.New("since must be a positive duration")
			}
		}
		if flags.Changed("since-time") {
			value, _ := flags.GetString("since-time")
			if _, err := time.Parse(time.RFC3339, value); err != nil || flags.Changed("since") {
				return nil, errors.New("use one valid since or since-time")
			}
		}
		_ = flags.Set("tail", fmt.Sprint(tail))
		_ = flags.Set("limit-bytes", fmt.Sprint(limit))
	}
	result := append([]string{verb}, resources...)
	flags.Visit(func(flag *pflag.Flag) { result = append(result, "--"+flag.Name+"="+flag.Value.String()) })
	return result, nil
}
