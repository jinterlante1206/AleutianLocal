package ast

// Dockerfile Tree-sitter Node Types
//
// This file documents the tree-sitter node types used by DockerfileParser for symbol extraction.
// The parser uses direct node traversal rather than tree-sitter's query language for
// more precise control over symbol extraction.
//
// Reference: https://github.com/camdencheek/tree-sitter-dockerfile

// Node type constants for Dockerfile AST traversal.
const (
	// Top-level node
	dockerfileNodeSourceFile = "source_file"
	dockerfileNodeComment    = "comment"

	// Instruction nodes
	dockerfileNodeFromInstruction        = "from_instruction"
	dockerfileNodeArgInstruction         = "arg_instruction"
	dockerfileNodeEnvInstruction         = "env_instruction"
	dockerfileNodeLabelInstruction       = "label_instruction"
	dockerfileNodeExposeInstruction      = "expose_instruction"
	dockerfileNodeVolumeInstruction      = "volume_instruction"
	dockerfileNodeWorkdirInstruction     = "workdir_instruction"
	dockerfileNodeCopyInstruction        = "copy_instruction"
	dockerfileNodeAddInstruction         = "add_instruction"
	dockerfileNodeRunInstruction         = "run_instruction"
	dockerfileNodeCmdInstruction         = "cmd_instruction"
	dockerfileNodeEntrypointInstruction  = "entrypoint_instruction"
	dockerfileNodeUserInstruction        = "user_instruction"
	dockerfileNodeHealthcheckInstruction = "healthcheck_instruction"

	// FROM components
	dockerfileNodeImageSpec  = "image_spec"
	dockerfileNodeImageName  = "image_name"
	dockerfileNodeImageTag   = "image_tag"
	dockerfileNodeImageAlias = "image_alias"

	// Value nodes
	dockerfileNodeEnvPair            = "env_pair"
	dockerfileNodeLabelPair          = "label_pair"
	dockerfileNodeUnquotedString     = "unquoted_string"
	dockerfileNodeDoubleQuotedString = "double_quoted_string"
	dockerfileNodeSingleQuotedString = "single_quoted_string"
	dockerfileNodeExposePort         = "expose_port"
	dockerfileNodePath               = "path"

	// Command nodes
	dockerfileNodeShellCommand    = "shell_command"
	dockerfileNodeShellFragment   = "shell_fragment"
	dockerfileNodeJsonStringArray = "json_string_array"
	dockerfileNodeJsonString      = "json_string"

	// Other nodes
	dockerfileNodeParam = "param"

	// Error nodes
	dockerfileNodeERROR = "ERROR"
)

// DockerfileNodeTypes maps symbol kinds to the tree-sitter node types that produce them.
var DockerfileNodeTypes = map[SymbolKind][]string{
	SymbolKindStage:       {dockerfileNodeFromInstruction},
	SymbolKindArg:         {dockerfileNodeArgInstruction},
	SymbolKindEnvVar:      {dockerfileNodeEnvInstruction},
	SymbolKindLabel:       {dockerfileNodeLabelInstruction},
	SymbolKindPort:        {dockerfileNodeExposeInstruction},
	SymbolKindVolume:      {dockerfileNodeVolumeInstruction},
	SymbolKindInstruction: {dockerfileNodeRunInstruction, dockerfileNodeCopyInstruction, dockerfileNodeAddInstruction},
}

// Dockerfile AST Structure Reference
//
// source_file
// ├── comment (# Comment)
// │
// ├── from_instruction
// │   ├── FROM
// │   ├── image_spec
// │   │   ├── image_name
// │   │   └── image_tag (optional)
// │   │       └── :tag
// │   ├── AS (optional)
// │   └── image_alias (stage name)
// │
// ├── arg_instruction
// │   ├── ARG
// │   ├── unquoted_string (name)
// │   ├── = (optional)
// │   └── unquoted_string (default value, optional)
// │
// ├── env_instruction
// │   ├── ENV
// │   └── env_pair
// │       ├── unquoted_string (name)
// │       ├── =
// │       └── unquoted_string/quoted_string (value)
// │
// ├── label_instruction
// │   ├── LABEL
// │   └── label_pair
// │       ├── unquoted_string (key)
// │       ├── =
// │       └── quoted_string (value)
// │
// ├── expose_instruction
// │   ├── EXPOSE
// │   └── expose_port
// │       └── port/protocol
// │
// ├── volume_instruction
// │   ├── VOLUME
// │   └── json_string_array / path
// │
// ├── run_instruction
// │   ├── RUN
// │   └── shell_command / json_string_array
// │
// └── ...

// Dockerfile Stages
//
// Multi-stage builds use FROM ... AS name syntax.
// Each stage is extracted as a SymbolKindStage symbol.
// The stage name (alias) is used for referencing in COPY --from=.

// Dockerfile ARG vs ENV
//
// ARG: Build-time variables, only available during build
// ENV: Runtime environment variables, persist in image
//
// Both are extracted with their default values when present.

// Dockerfile EXPOSE
//
// EXPOSE declares ports the container listens on:
// - EXPOSE 8080 (default TCP)
// - EXPOSE 8080/tcp
// - EXPOSE 53/udp
//
// The port and protocol are extracted.

// Dockerfile VOLUME
//
// VOLUME declares mount points:
// - VOLUME /data
// - VOLUME ["/data", "/config"]
//
// Multiple paths can be in a single instruction.
