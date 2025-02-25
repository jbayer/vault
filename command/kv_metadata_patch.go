package command

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mitchellh/cli"
	"github.com/posener/complete"
)

var (
	_ cli.Command             = (*KVMetadataPutCommand)(nil)
	_ cli.CommandAutocomplete = (*KVMetadataPutCommand)(nil)
)

type KVMetadataPatchCommand struct {
	*BaseCommand

	flagMaxVersions        int
	flagCASRequired        BoolPtr
	flagDeleteVersionAfter time.Duration
	flagCustomMetadata     map[string]string
	testStdin              io.Reader // for tests
}

func (c *KVMetadataPatchCommand) Synopsis() string {
	return "Patches key settings in the KV store"
}

func (c *KVMetadataPatchCommand) Help() string {
	helpText := `
Usage: vault metadata kv patch [options] KEY

  This command can be used to create a blank key in the key-value store or to
  update key configuration for a specified key.

  Create a key in the key-value store with no data:

      $ vault kv metadata patch secret/foo

  Set a max versions setting on the key:

      $ vault kv metadata patch -max-versions=5 secret/foo

  Set delete-version-after on the key:

      $ vault kv metadata patch -delete-version-after=3h25m19s secret/foo

  Require Check-and-Set for this key:

      $ vault kv metadata patch -cas-required secret/foo

  Set custom metadata on the key:

      $ vault kv metadata patch -custom-metadata=foo=abc -custom-metadata=bar=123 secret/foo

  Additional flags and more advanced use cases are detailed below.

` + c.Flags().Help()
	return strings.TrimSpace(helpText)
}

func (c *KVMetadataPatchCommand) Flags() *FlagSets {
	set := c.flagSet(FlagSetHTTP | FlagSetOutputFormat)

	// Common Options
	f := set.NewFlagSet("Common Options")

	f.IntVar(&IntVar{
		Name:    "max-versions",
		Target:  &c.flagMaxVersions,
		Default: -1,
		Usage:   `The number of versions to keep. If not set, the backend’s configured max version is used.`,
	})

	f.BoolPtrVar(&BoolPtrVar{
		Name:   "cas-required",
		Target: &c.flagCASRequired,
		Usage:  `If true the key will require the cas parameter to be set on all write requests. If false, the backend’s configuration will be used.`,
	})

	f.DurationVar(&DurationVar{
		Name:       "delete-version-after",
		Target:     &c.flagDeleteVersionAfter,
		Default:    -1,
		EnvVar:     "",
		Completion: complete.PredictAnything,
		Usage: `Specifies the length of time before a version is deleted.
		If not set, the backend's configured delete-version-after is used. Cannot be
		greater than the backend's delete-version-after. The delete-version-after is
		specified as a numeric string with a suffix like "30s" or
		"3h25m19s".`,
	})

	f.StringMapVar(&StringMapVar{
		Name:    "custom-metadata",
		Target:  &c.flagCustomMetadata,
		Default: map[string]string{},
		Usage: `Specifies arbitrary version-agnostic key=value metadata meant to describe a secret.
		This can be specified multiple times to add multiple pieces of metadata.`,
	})

	return set
}

func (c *KVMetadataPatchCommand) AutocompleteArgs() complete.Predictor {
	return nil
}

func (c *KVMetadataPatchCommand) AutocompleteFlags() complete.Flags {
	return c.Flags().Completions()
}

func (c *KVMetadataPatchCommand) Run(args []string) int {
	f := c.Flags()

	if err := f.Parse(args); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	args = f.Args()

	switch {
	case len(args) < 1:
		c.UI.Error(fmt.Sprintf("Not enough arguments (expected 1, got %d)", len(args)))
		return 1
	case len(args) > 1:
		c.UI.Error(fmt.Sprintf("Too many arguments (expected 1, got %d)", len(args)))
		return 1
	}

	client, err := c.Client()
	if err != nil {
		c.UI.Error(err.Error())
		return 2
	}

	path := sanitizePath(args[0])

	mountPath, v2, err := isKVv2(path, client)
	if err != nil {
		c.UI.Error(err.Error())
		return 2
	}
	if !v2 {
		c.UI.Error("Metadata not supported on KV Version 1")
		return 1
	}

	path = addPrefixToVKVPath(path, mountPath, "metadata")

	data := map[string]interface{}{}

	if c.flagMaxVersions >= 0 {
		data["max_versions"] = c.flagMaxVersions
	}

	if c.flagCASRequired.IsSet() {
		data["cas_required"] = c.flagCASRequired.Get()
	}

	if c.flagDeleteVersionAfter >= 0 {
		data["delete_version_after"] = c.flagDeleteVersionAfter.String()
	}

	if len(c.flagCustomMetadata) > 0 {
		data["custom_metadata"] = c.flagCustomMetadata
	}

	secret, err := client.Logical().JSONMergePatch(context.Background(), path, data)
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error writing data to %s: %s", path, err))

		if secret != nil {
			OutputSecret(c.UI, secret)
		}
		return 2
	}

	if secret == nil {
		// Don't output anything unless using the "table" format
		if Format(c.UI) == "table" {
			c.UI.Info(fmt.Sprintf("Success! Data written to: %s", path))
		}
		return 0
	}

	return OutputSecret(c.UI, secret)
}
