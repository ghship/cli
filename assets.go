package ghship

import "embed"

// Skill is shipped with the CLI so installed guidance matches its version.
//
//go:embed SKILL.md references/cli.md references/pipeline.md references/setup.md references/workflow.md
var Skill embed.FS

// Version is the release version; npm metadata is checked during packaging.
const Version = "1.0.0"
