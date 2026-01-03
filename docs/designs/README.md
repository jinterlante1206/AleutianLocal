# Design Documents

This folder contains architecture and design documents for the Aleutian project.

## Folder Structure

```
docs/designs/
├── README.md           # This file
├── pending/            # Designs awaiting approval or implementation
└── completed/          # Implemented designs (kept for reference)
```

## Workflow

1. **New designs** go in `pending/`
2. After **implementation is complete and verified**, move to `completed/`
3. Keep completed designs for historical reference and onboarding

## Document Status

Each design doc should include a status field:

| Status | Meaning |
|--------|---------|
| `DRAFT` | Initial draft, not ready for review |
| `PENDING APPROVAL` | Ready for architecture review |
| `APPROVED` | Approved, ready for implementation |
| `IN PROGRESS` | Currently being implemented |
| `COMPLETED` | Fully implemented, move to completed/ |

## Current Documents

### Pending

| Document | Status | Description |
|----------|--------|-------------|
| [streaming_chat_integration.md](pending/streaming_chat_integration.md) | PENDING APPROVAL | CLI ChatRunner refactor with graceful shutdown |
| [orchestrator_sse_streaming.md](pending/orchestrator_sse_streaming.md) | PENDING APPROVAL | Server-side SSE streaming for chat endpoints |

### Completed

_No completed designs yet._
