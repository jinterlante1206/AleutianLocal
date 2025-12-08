# Aleutian Engineering & Testing Guide

## 1. System Architecture
Aleutian operates as a local Secure Memory Gateway, prioritizing data sovereignty through a strict separation of reasoning and execution.

### Architectural Components
* **Reasoning Engine (Python Container):** A stateless service that constructs prompts and plans task execution. It has no direct access to the host filesystem and communicates solely by emitting JSON instructions (e.g., tool calls).
* **Execution Engine (Go CLI):** A stateful client that receives instructions from the reasoning engine. It validates all requests against the Policy Engine (regex/logic rules) before executing file I/O operations.
* **Persistent Storage (Weaviate):** Stores vector embeddings partitioned by `data_space`.

## 2. Testing Strategy
The project utilizes black-box end-to-end (E2E) integration testing. Release tests compile the source code into a binary and execute it against the live container stack to verify actual behavior without mocks.

### Coverage Levels
1.  **Unit Tests (`go test ./cmd/...`)**
    * Scope: Internal logic, security path validation (`isPathAllowed`), and JSON parsing.
    * Frequency: Execute on every commit.

2.  **Integration Tests (`go test ./test/e2e/...`)**
    * Scope: Full system verification. Compiles the CLI binary, connects to the local Podman stack, and executes live commands (`populate`, `ask`, `trace`).
    * Frequency: Execute before releases or major merges.

## 3. Execution Instructions

### Prerequisites
* Podman and Podman Compose must be installed.
* The stack must be running (`aleutian stack start`) and healthy.
* If using the local backend, Ollama must be running.

### Run Full Suite
This command builds a temporary test binary and executes all E2E scenarios.

```bash
go test ./test/e2e/... -v