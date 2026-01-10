# Aleutian Local: Architecture & Philosophy

## The Problem We're Solving

Modern AI assistants are powerful, but they come with a fundamental tradeoff: **to use them, you must surrender your data**.

Every question you ask ChatGPT, every document you upload to Claude, every code snippet you paste into Copilot—all of it leaves your infrastructure and enters someone else's servers. For individuals, this might be acceptable. For enterprises, it's often a deal-breaker.

Consider these scenarios:

- **The Law Firm**: Attorneys need AI help analyzing contracts, but client-attorney privilege prohibits sharing documents with third parties.
- **The Healthcare Provider**: Doctors want AI to help with diagnoses, but HIPAA makes it illegal to send patient data to external services.
- **The Defense Contractor**: Engineers need code assistance, but classified information cannot touch commercial cloud infrastructure.
- **The Startup**: Founders want to use AI for competitive analysis, but their proprietary strategies would become training data for their competitors.

The current solutions are inadequate:

| Approach | Problem |
|----------|---------|
| "Enterprise" cloud tiers | Still send data externally; trust is outsourced |
| On-premise LLMs | Complex to deploy; no security guardrails |
| Air-gapped networks | Crippling restrictions; poor user experience |
| Manual redaction | Human error; doesn't scale |

**Aleutian was built to solve this.**

---

## Core Philosophy

### 1. Data Sovereignty as an Architectural Boundary

Aleutian treats data locality not as a feature, but as an **architectural invariant**. Your data never leaves your infrastructure—not because of a policy, but because the system physically cannot transmit it.

```
┌──────────────────────────────────────────────────────────────────┐
│                     YOUR INFRASTRUCTURE                          │
│                                                                  │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────────┐     │
│  │   Your      │────►│  Aleutian   │────►│  Local Ollama   │     │
│  │   Data      │     │  Gateway    │     │  (Your Models)  │     │
│  └─────────────┘     └─────────────┘     └─────────────────┘     │
│         │                   │                                    │
│         │                   ▼                                    │
│         │            ┌─────────────┐                             │
│         └───────────►│  Weaviate   │                             │
│                      │  (Your DB)  │                             │
│                      └─────────────┘                             │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                      ┌───────────────┐
                      │   NOTHING     │  ← No data crosses this boundary
                      │   LEAVES      │
                      └───────────────┘
```

Even when you choose to use cloud LLMs (OpenAI, Anthropic), Aleutian's **Policy Engine** intercepts every request and blocks sensitive patterns before transmission.

### 2. Security by Compilation, Not Configuration

Most security tools rely on configuration files that can be deleted, modified, or ignored. Aleutian embeds security policies **directly into the binary** using Go's `embed` directive.

```go
//go:embed data_classification_patterns.yaml
var embeddedPolicies []byte
```

This means:
- You cannot disable security by deleting a config file
- The binary's SHA-256 hash proves which policies are active
- Audit trails are cryptographically verifiable

### 3. Tamper-Evident Audit Logging

Every conversation in Aleutian is protected by a **hash chain**—the same cryptographic technique that secures blockchains.

```
Turn 1: "What's our revenue?"
  Hash: SHA256(question + answer) = abc123
  PrevHash: null

Turn 2: "Break it down by region"
  Hash: SHA256(question + answer + "abc123") = def456
  PrevHash: abc123

Turn 3: "Show me the Asia numbers"
  Hash: SHA256(question + answer + "def456") = ghi789
  PrevHash: def456
```

If anyone modifies a single character in the conversation history, the chain breaks. This provides:
- **Integrity**: Proof that logs haven't been tampered with
- **Non-repudiation**: Cryptographic evidence of who said what, when
- **Compliance**: Audit trails that satisfy SOX, HIPAA, MiFID II

### 4. Fail-Closed Security Model

When Aleutian encounters ambiguity, it **blocks by default**. This is the opposite of most software, which tends to fail open for user convenience.

```go
// From policy_engine.go
func (e *PolicyEngine) CheckContent(content string) Decision {
    for _, rule := range e.rules {
        if rule.Pattern.MatchString(content) {
            return Decision{
                Action: BLOCK,  // Always block on match
                Reason: rule.Description,
            }
        }
    }
    return Decision{Action: ALLOW}
}
```

If the Policy Engine fails to load, Aleutian refuses to start. If pattern matching times out, the request is blocked. There is no "emergency bypass."

### 5. Interface-First Architecture

Every component in Aleutian is defined by its **interface**, not its implementation. This enables:
- **Testability**: Mock any component for unit tests
- **Extensibility**: Swap implementations without changing code
- **Enterprise customization**: Plug in HSMs, SIEM, custom auth

```go
// From infrastructure_manager.go
type InfrastructureManager interface {
    EnsureReady(ctx context.Context, opts InfrastructureOptions) error
    ValidateMounts() (*MountValidationResult, error)
    GetMachineStatus() (*MachineStatus, error)
    Stop(ctx context.Context) error
}

// Production implementation
type DefaultInfrastructureManager struct { ... }

// Test implementation
type MockInfrastructureManager struct { ... }

// Enterprise implementation with HSM
type EnterpriseInfrastructureManager struct { ... }
```

---

## Technical Architecture

### System Overview

Aleutian is a **microservices architecture** deployed via Podman Compose, orchestrated by a single Go CLI binary.

```
┌───────────────────────────────────────────────────────────────────────────────┐
│                              ALEUTIAN CLI (Go)                                │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────────────┐  │
│  │StackManager  │ │ProfileResolver│ │SecretsManager│ │DiagnosticsCollector │  │
│  │  (Phase 10)  │ │  (Phase 4)   │ │  (Phase 6)   │ │     (Phase 3)        │  │
│  └──────────────┘ └──────────────┘ └──────────────┘ └──────────────────────┘  │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────────────┐  │
│  │HealthChecker │ │ComposeExecutor││InfraManager  │ │  ProcessManager      │  │
│  │  (Phase 9)   │ │  (Phase 8)   │ │  (Phase 5)   │ │     (Phase 1)        │  │
│  └──────────────┘ └──────────────┘ └──────────────┘ └──────────────────────┘  │
└───────────────────────────────────────────────────────────────────────────────┘
                                       │
                                       ▼
┌───────────────────────────────────────────────────────────────────────────────┐
│                           CONTAINER NETWORK                                   │
│                                                                               │
│  ┌────────────────────────────────────────────────────────────────────────┐   │
│  │                      ORCHESTRATOR (Go)                                 │   │
│  │  • SSE Streaming endpoints                                             │   │
│  │  • Policy Engine (compiled-in regex patterns)                          │   │
│  │  • LLM Client abstraction (Ollama/OpenAI/Anthropic)                    │   │
│  │  • Session verification with hash chains                               │   │
│  │  • OpenTelemetry instrumentation                                       │   │
│  └────────────────────────────────────────────────────────────────────────┘   │
│                    │                              │                           │
│                    ▼                              ▼                           │
│  ┌────────────────────────────────┐  ┌────────────────────────────────────┐   │
│  │      RAG ENGINE (Python)       │  │         WEAVIATE (Go)              │   │
│  │  • LangChain/LlamaIndex        │  │  • Vector similarity search        │   │
│  │  • Autonomous code agent       │  │  • HNSW indexing                   │   │
│  │  • Cross-encoder reranking     │  │  • Session/Conversation schema     │   │
│  └────────────────────────────────┘  └────────────────────────────────────┘   │
│                                                                               │
│  ┌────────────────────────────────┐  ┌────────────────────────────────────┐   │
│  │    EMBEDDING SERVER (Python)   │  │      OLLAMA (Go + C++)             │   │
│  │  • sentence-transformers       │  │  • Local model inference           │   │
│  │  • Batch embedding API         │  │  • NDJSON streaming                │   │
│  └────────────────────────────────┘  └────────────────────────────────────┘   │
└───────────────────────────────────────────────────────────────────────────────┘
```

### Component Deep Dive

#### 1. StackManager (Phase 10) — The Orchestrator

The StackManager coordinates all lifecycle operations through a **6-phase startup sequence**:

```go
func (m *DefaultStackManager) Start(ctx context.Context, opts StartOptions) error {
    // Phase 1: Infrastructure
    // Provision/verify Podman machine (macOS), validate mounts
    if err := m.infra.EnsureReady(ctx, infraOpts); err != nil {
        return m.handlePhaseFailure(ctx, "infrastructure", err)
    }

    // Phase 2: Model Verification
    // Ensure required Ollama models are pulled
    if !opts.SkipModelCheck && m.models != nil {
        if err := m.models.EnsureModels(ctx); err != nil {
            return m.handlePhaseFailure(ctx, "models", err)
        }
    }

    // Phase 3: Secrets
    // Provision API keys via interactive prompts or environment
    if err := m.secrets.EnsureSecrets(ctx, secretOpts); err != nil {
        return m.handlePhaseFailure(ctx, "secrets", err)
    }

    // Phase 4: Cache Resolution
    // Determine model cache paths (HuggingFace, Ollama, Transformers)
    paths := m.cache.ResolvePaths()

    // Phase 5: Profile Resolution
    // Detect hardware → select optimal model configuration
    profile, envVars := m.profile.ResolveProfile(opts.Profile)

    // Phase 6: Compose Up
    // Start containers with resolved environment
    if err := m.compose.Up(ctx, composeOpts); err != nil {
        return m.handlePhaseFailure(ctx, "compose", err)
    }

    // Phase 7: Health Check
    // Wait for all services to become healthy
    return m.health.WaitForHealthy(ctx, healthOpts)
}
```

**Key Features**:
- **Panic recovery**: Every goroutine is wrapped in `SafeGo()` to prevent crashes
- **Context propagation**: All operations respect cancellation
- **Diagnostic collection**: On failure, system state is captured for debugging
- **Mutex serialization**: Concurrent Start/Stop/Destroy operations are serialized

#### 2. InfrastructureManager (Phase 5) — The Security Boundary

This component controls the **attack surface** between host and containers.

```go
// Mount validation prevents directory traversal attacks
func (m *DefaultInfrastructureManager) ValidateMounts() (*MountValidationResult, error) {
    result := &MountValidationResult{Valid: true}

    for _, mount := range m.config.Drives {
        // Resolve symlinks to prevent escape
        realPath, err := filepath.EvalSymlinks(mount)
        if err != nil {
            result.Errors = append(result.Errors, MountError{
                Path:   mount,
                Reason: "symlink resolution failed",
            })
            result.Valid = false
            continue
        }

        // Reject paths outside allowed directories
        if !m.isPathAllowed(realPath) {
            result.Errors = append(result.Errors, MountError{
                Path:   mount,
                Reason: "path outside allowed directories",
            })
            result.Valid = false
        }
    }

    return result, nil
}
```

**Security Features**:
- **Mount Guard**: Validates all volume mounts before container start
- **Foreign Workload Detection**: Alerts if non-Aleutian containers are running
- **Network Kill Switch**: Can isolate containers on security events
- **Audit Trail**: All infrastructure operations are logged with timestamps

#### 3. Policy Engine — The Firewall

The Policy Engine is Aleutian's **first line of defense** against data exfiltration.

```go
// Patterns are compiled at build time, not runtime
type PolicyEngine struct {
    rules []CompiledRule  // Loaded from embedded YAML
}

type CompiledRule struct {
    Pattern     *regexp.Regexp
    Category    string    // API_KEY, PII, SECRET, CUSTOM
    Severity    string    // HIGH, MEDIUM, LOW
    Description string
}

// Called on EVERY request—chat, ingest, agent query
func (e *PolicyEngine) Scan(content string) []Finding {
    var findings []Finding

    for _, rule := range e.rules {
        matches := rule.Pattern.FindAllStringIndex(content, -1)
        for _, match := range matches {
            findings = append(findings, Finding{
                Category: rule.Category,
                Severity: rule.Severity,
                Position: match,
                Redacted: redact(content[match[0]:match[1]]),
            })
        }
    }

    return findings
}
```

**Built-in Patterns**:
| Category | Examples |
|----------|----------|
| API Keys | `sk-...`, `AKIA...`, `ghp_...` |
| Private Keys | `-----BEGIN RSA PRIVATE KEY-----` |
| AWS Secrets | `aws_access_key_id`, `aws_secret_access_key` |
| Database URLs | `postgres://`, `mysql://`, `mongodb://` |
| PII | SSN patterns, credit card numbers |

#### 4. Streaming Architecture (Phase 12-13)

Aleutian implements **Server-Sent Events (SSE)** for real-time token streaming.

```
Client                    Orchestrator                 Ollama
   │                           │                          │
   │  POST /v1/chat/rag        │                          │
   │  stream: true             │                          │
   │ ─────────────────────────►│                          │
   │                           │                          │
   │                           │  POST /api/chat          │
   │                           │  stream: true            │
   │                           │ ────────────────────────►│
   │                           │                          │
   │  SSE: {"type":"status"}   │◄─ NDJSON: {"done":false} │
   │◄──────────────────────────│                          │
   │                           │                          │
   │  SSE: {"type":"token",    │◄─ NDJSON: {"message":..} │
   │        "content":"The"}   │                          │
   │◄──────────────────────────│                          │
   │                           │                          │
   │  SSE: {"type":"thinking", │◄─ NDJSON: {"thinking":..}│
   │        "content":"Let me"}│     (thinking models)    │
   │◄──────────────────────────│                          │
   │                           │                          │
   │  SSE: {"type":"done",     │◄─ NDJSON: {"done":true}  │
   │        "session_id":...}  │                          │
   │◄──────────────────────────│                          │
```

**Protocol Details**:
- **Ollama**: NDJSON (newline-delimited JSON), one object per line
- **OpenAI/Anthropic**: SSE with `data:` prefix
- **Internal**: Unified `StreamEvent` type normalizes all backends

```go
type StreamEvent struct {
    Id        string          `json:"id"`
    CreatedAt int64           `json:"created_at"`
    Type      StreamEventType `json:"type"`      // token, thinking, sources, done, error
    Content   string          `json:"content,omitempty"`

    // Hash chain fields for integrity
    Hash      string          `json:"hash,omitempty"`
    PrevHash  string          `json:"prev_hash,omitempty"`
}
```

#### 5. Session Integrity (Phase 13)

Every conversation is protected by cryptographic hash chains.

```go
// From integrity.go
type fullChainVerifier struct {
    hasher HashComputer
}

func (v *fullChainVerifier) Verify(events []StreamEvent) *ChainVerificationResult {
    result := &ChainVerificationResult{Verified: true}

    var prevHash string
    for i, event := range events {
        // Recompute expected hash
        expectedHash := v.hasher.ComputeEventHash(
            event.Content,
            event.CreatedAt,
            prevHash,
        )

        // Compare with stored hash
        if event.Hash != expectedHash {
            result.Verified = false
            result.FailedAt = i
            result.Error = fmt.Sprintf(
                "hash mismatch at event %d: expected %s, got %s",
                i, expectedHash, event.Hash,
            )
            return result
        }

        // Verify chain link
        if event.PrevHash != prevHash {
            result.Verified = false
            result.FailedAt = i
            result.Error = fmt.Sprintf(
                "chain break at event %d: expected prev %s, got %s",
                i, prevHash, event.PrevHash,
            )
            return result
        }

        prevHash = event.Hash
    }

    result.ChainHash = prevHash
    return result
}
```

**Enterprise Extensions**:

| Interface | Purpose | Implementation |
|-----------|---------|----------------|
| `KeyedHashComputer` | HMAC with key rotation | Vault, AWS KMS, Azure KeyVault |
| `SignatureVerifier` | Digital signatures | RSA, ECDSA, Ed25519 via PKCS#11 |
| `TimestampAuthority` | RFC 3161 timestamps | DigiCert, GlobalSign, Sectigo |
| `HSMProvider` | Hardware security module | PKCS#11, CloudHSM, Thales Luna |
| `AuditLogger` | SIEM integration | Splunk, DataDog, Elastic |

---

## Feature Highlights

### What No One Else Has

These features are architecturally unique to Aleutian—you won't find them in LangChain, LlamaIndex, or other AI frameworks.

| Feature | What It Does | Why It Matters |
|---------|--------------|----------------|
| **Cryptographic Hash Chain** | Every conversation turn is SHA-256 linked: `H_n = Hash(Data + H_{n-1})` | Creates blockchain-style tamper-evident audit logs. If someone edits conversation history, the chain breaks. |
| **Compiled-In Security Policies** | DLP patterns are embedded in the binary via Go's `embed` directive | Cannot be disabled by deleting config files. Binary hash proves which policies are active. |
| **On-Demand Air-Gap Verification** | Active probe (`VerifyNetworkIsolation`) proves offline status via DNS/TCP checks | Regulated industries can **mathematically prove** their stack never touched the internet. |
| **HealthIntelligence** | Local LLM analyzes system logs and explains degradation in plain English | "Weaviate is slow because disk is 95% full" instead of cryptic error codes. |
| **Buffered Streaming with DLP** | Two-mode streaming (Sentence vs. Full Buffer) allows PII scanning on LLM output | Other tools stream raw tokens—Aleutian can scan output for secrets before display. |
| **Zero-Log Guarantee** | Architectural enforcement that secret values are **never** logged, only access events | Even debug logs can't leak credentials. Required for SOC 2/HIPAA. |

### Self-Healing & Automation

| Feature | What It Does | Why It Matters |
|---------|--------------|----------------|
| **InfrastructureManager** | Detects broken Podman VMs, stale mounts, and automatically repairs them | "It just works" after laptop sleep/wake cycles that corrupt Docker. |
| **CachePathResolver** | Auto-detects external drives and prioritizes them for model caches | Plugs in your 2TB SSD → Aleutian automatically uses it for 70B models. |
| **ModelEnsurer** | Checks required models on startup, pulls missing ones with progress bars | Users never need to manually run `ollama pull`. Stack handles it. |
| **Stale Lock Cleanup** | Automatically removes `.lock` files from failed downloads | Prevents "Stuck Download" issues caused by previous crashes. |
| **ForceCleanup Strategy** | "Nuclear option" to aggressively scrub containers when normal stops fail | Guarantees clean slate even if Podman runtime is corrupted. |
| **Panic Recovery Handler** | "Black Box" recorder captures system state exactly when crashes occur | Like an airplane's flight recorder for debugging catastrophic failures. |

### Security & Compliance

| Feature | What It Does | Why It Matters |
|---------|--------------|----------------|
| **SecretsManager** | Retrieves secrets from Env, macOS Keychain, 1Password, or Linux Libsecret | No cleartext API keys in config files. Ever. |
| **Log Sanitization** | Regex-based PII redaction (Email, IP, Keys) before logs go to LLM | Safe to use AI for log analysis without leaking user data. |
| **Security Hardening Defaults** | ReadOnlyMounts, DropCapabilities, NetworkIsolation by default | Compromised container cannot modify user files or phone home. |
| **"Empty Socket" Pattern** | Interface segregation for enterprise features (TPM, Vault, HSM) | Enterprise can plug in FIPS-validated crypto without forking code. |

### Developer Experience

| Feature | What It Does | Why It Matters |
|---------|--------------|----------------|
| **ProcessManager** | Unified wrapper for `os/exec` with context cancellation and audit logging | 100% testability—can mock every CLI command for unit tests. |
| **DiagnosticsCollector** | Generates standardized debug bundles with logs, metrics, and system state | Solves "it works on my machine" with reproducible bug reports. |
| **UserPrompter** | Abstracted terminal I/O with `--non-interactive` and `--yes` flags | Scriptable for CI/CD while preserving rich interactive UX for humans. |
| **ProfileResolver** | Auto-detects RAM/GPU to select optimal model configuration | Users get best model for their hardware without manual tuning. |
| **Structured Errors** | Rich error types (`ERR_WEAVIATE_UNAVAILABLE`) with remediation steps | Tells users exactly how to fix problems, not just that something broke. |

---

## Reliability & Security Hardening

Aleutian addresses **28 identified anti-patterns** across seven categories:

### Go-Specific Pitfalls (5 items)
- **Nil Interface Panic Prevention**: No-op implementations prevent nil dereference
- **Goroutine Panic Recovery**: `SafeGo()` wrapper catches panics
- **Goroutine Leak Prevention**: `GoroutineTracker` monitors lifecycle
- **Default Timeout Enforcement**: No unbounded operations
- **Error Wrapping Standards**: Consistent `%w` wrapping for stack traces

### Concurrency & Resource Risks (3 items)
- **Bounded Channels**: All channels have explicit capacity
- **File Descriptor Limits**: Explicit `ulimit` checks
- **Observer Resource Drain**: Metrics sampling prevents cardinality explosion

### State Management Risks (4 items)
- **Double CLI Prevention**: PID file locking prevents race conditions
- **Zombie State Recovery**: Partial failures trigger automatic cleanup
- **Split-Brain Detection**: Container state reconciliation
- **Configuration Drift**: Hash-based drift detection

### Security & Supply Chain Risks (7 items)
- **Explicit EnvVars Type**: No ambient variable leakage
- **Secret Sprawl Mitigation**: Centralized secret management
- **Memory Protection**: Sensitive data cleared after use
- **Non-Privileged Default**: Containers run as non-root
- **Immutable Tags**: SHA256 digest pinning
- **Volume Namespacing**: Tenant isolation
- **File Permission Hardening**: 0600 for secrets, 0755 for binaries

### Observability & UX Risks (6 items)
- **Circuit Breaker Pattern**: Cascading failure prevention
- **Cardinality Bounds**: Label value limits
- **Data Retention Policy**: Automatic log rotation
- **Progress Indication**: No silent hangs
- **Destructive Confirmation**: `--force` required for deletions
- **Error Deduplication**: Prevents log spam

---

## Performance Characteristics

### Memory Profiles

| Profile | RAM | Default Model | Context Window |
|---------|-----|---------------|----------------|
| Low | 8-15GB | gemma3:4b | 2,048 |
| Standard | 16-31GB | qwen3:14b | 4,096 |
| Performance | 32-127GB | gpt-oss:20b | 8,192 |
| Ultra | 128GB+ | gpt-oss:120b | 32,768 |

### Latency Targets

| Operation | P50 | P99 |
|-----------|-----|-----|
| First token (streaming) | 200ms | 800ms |
| Policy scan (per request) | 5ms | 50ms |
| Vector search (top-10) | 50ms | 200ms |
| Session verification | 100ms | 500ms |

### Throughput

| Metric | Target |
|--------|--------|
| Concurrent chat sessions | 100 |
| Document ingestion | 1000 chunks/sec |
| Embedding batch size | 32 texts |
| SSE events/second | 50 |

---

## Compliance Matrix

| Requirement | How Aleutian Addresses It | Enable With |
|-------------|---------------------------|-------------|
| **HIPAA** (Healthcare) | Data never leaves infrastructure; audit logs; access controls | `aleutian stack start` (local-only mode) ⚠️ |
| **SOC 2** (Security) | Hash chain integrity; RBAC; encryption at rest | `aleutian session verify --full` |
| **PCI-DSS** (Finance) | HSM support; key management; network segmentation | Configure `session_integrity.enterprise.hsm` in aleutian.yaml |
| **GDPR** (Privacy) | Right to erasure; data minimization; consent tracking | `aleutian session delete <id>` + policy engine |
| **MiFID II** (Trading) | RFC 3161 timestamps; 7-year retention; immutable logs | Configure `session_integrity.enterprise.tsa` in aleutian.yaml |
| **FIPS 140-2** (Government) | PKCS#11 HSM integration; validated crypto | Configure `session_integrity.enterprise.hsm.provider: pkcs11` |
| **21 CFR Part 11** (Pharma) | Digital signatures; audit trails; access controls | `session_integrity.enterprise.hmac.enabled: true` + SIEM |

> ⚠️ **Important**: Compliance guarantees marked with ⚠️ are **invalidated** when using external LLM APIs (OpenAI, Anthropic). These features require local-only deployment:
> - **HIPAA**: Data residency is only guaranteed with `model_backend.type: ollama`
> - **PCI-DSS**: Card data must never leave your infrastructure
> - **GDPR**: Right to erasure cannot be guaranteed with external APIs
>
> To ensure compliance, verify your configuration:
> ```bash
> # Check current backend (should be "ollama" for compliance)
> aleutian config get model_backend.type
> ```

---

## Getting Started

```bash
# Install
brew tap jinterlante1206/aleutian
brew install aleutian

# Start (auto-detects optimal profile)
aleutian stack start

# Chat with your data
aleutian chat

# Verify session integrity
aleutian session verify <session_id>
```

---

## Further Reading

- [README.md](README.md) — Installation and command reference
- [EXAMPLES.md](EXAMPLES.md) — Detailed usage examples
- [docs/designs/](docs/designs/) — Design documents for each phase
- [docs/designs/completed/enterprise_integrity_extensions.md](docs/designs/completed/enterprise_integrity_extensions.md) — Enterprise extension interfaces
