
# Design Document: Verified Pipeline Refactor

**Status**: COMPLETE
**Date**: 2026-01-11
**Completed**: 2026-01-13
**Author**: John
**Severity**: All issues resolved

---

## Executive Summary

A comprehensive code review of `services/rag_engine/pipelines/verified.py` has identified **17 issues** ranging from critical logic bugs to architectural inefficiencies. This document catalogs all findings and proposes fixes.

| Severity | Count | Description |
|----------|-------|-------------|
| P0 (Critical) | 4 | Logic bugs that cause incorrect behavior |
| P1 (High) | 5 | Bugs that cause failures or security issues |ok let'
| P2 (Medium) | 4 | Code quality and maintainability issues |
| P3 (Low) | 4 | Minor improvements and best practices |

---

## P0: Critical Bugs (Must Fix)

### P0-1: `_build_refiner_prompt()` is Identical to `_build_skeptic_prompt()`

**Location**: Lines 88-121

**Problem**: The refiner prompt is a verbatim copy of the skeptic prompt. The refiner should produce a **rewritten answer**, but instead asks for a JSON audit verdict.

```python
# CURRENT (BROKEN) - refiner prompt asks for JSON:
def _build_refiner_prompt(self, req: RefinerRequest) -> str:
    return f"""
    You are a SKEPTICAL FACT-CHECKER auditing someone else's answer...
    Output ONLY valid JSON:
    {{
        "is_verified": boolean,
        ...
    }}
    """
```

**Impact**:
- `_execute_refinement()` returns JSON string instead of refined answer
- `state.current_answer` becomes malformed JSON
- Subsequent skeptic audits evaluate JSON syntax, not content
- Verification loop never converges correctly

**Root Cause**: Copy-paste error during implementation.

**Fix**: Implement a proper refiner prompt:

```python
def _build_refiner_prompt(self, req: RefinerRequest) -> str:
    """Builds prompt for the Refiner to rewrite the answer."""
    hallucination_list = "\n".join(f"- {h}" for h in req.audit_result.hallucinations)

    return f"""You are a CAREFUL ANSWER REFINER. Your task is to rewrite an answer
to remove hallucinations while keeping verified facts.

ORIGINAL QUERY: {req.query}

DRAFT ANSWER (contains hallucinations):
{req.draft_answer}

HALLUCINATIONS TO REMOVE:
{hallucination_list}

SKEPTIC'S REASONING:
{req.audit_result.reasoning}

INSTRUCTIONS:
1. Keep all claims that ARE supported by the evidence
2. Remove or qualify claims marked as hallucinations
3. If removing claims leaves nothing, say "Based on the available documents, I cannot fully answer this question."
4. Do NOT add new information not in the original answer
5. Maintain a helpful, natural tone

Write ONLY the refined answer (no JSON, no explanation):"""
```

---

### P0-2: `_build_refiner_prompt()` References Wrong Field Names

**Location**: Lines 88-121

**Problem**: The prompt references `req.proposed_answer` and `req.evidence_text`, but `RefinerRequest` has different fields:

```python
# RefinerRequest definition (from datatypes/verified.py):
class RefinerRequest(BaseModel):
    query: str
    draft_answer: str           # NOT proposed_answer!
    audit_result: SkepticAuditResult  # NOT evidence_text!
```

**Impact**: `AttributeError: 'RefinerRequest' object has no attribute 'proposed_answer'` at runtime.

**Fix**: Use correct field names matching the datatype.

---

### P0-3: Error Handling Defaults to `is_verified=True`

**Location**: Lines 146-152

**Problem**: When JSON parsing fails, the code defaults to marking the answer as **verified**:

```python
except Exception as e:
    logger.warning(f"Skeptic failed to parse JSON: {e}. Defaulting to verified.")
    return SkepticAuditResult(
        is_verified=True,  # DANGEROUS: Unverified answer marked verified!
        reasoning="JSON Parse Failure",
        hallucinations=[]
    )
```

**Impact**:
- Malformed LLM responses bypass verification
- Hallucinated content passes through undetected
- Violates fail-safe principle

**Security Implications**: An adversarial LLM response could intentionally produce unparseable JSON to bypass fact-checking.

**Fix**: Default to `is_verified=False` (fail-safe):

```python
except Exception as e:
    logger.error(f"Skeptic JSON parse failed: {e}. Failing safe to unverified.")
    return SkepticAuditResult(
        is_verified=False,  # Fail-safe: require re-verification
        reasoning=f"JSON parse failure: {str(e)[:100]}",
        hallucinations=["Unable to verify due to parse error"]
    )
```

---

### P0-4: `_log_debate()` Accesses Non-Existent Field

**Location**: Line 266

**Problem**: Attempts to access `state.history[0].proposed_answer`, but `history` contains `SkepticAuditResult` objects which have no `proposed_answer` field.

```python
# SkepticAuditResult fields:
class SkepticAuditResult(BaseModel):
    is_verified: bool
    reasoning: str
    hallucinations: List[str]
    missing_evidence: List[str]
    # NO proposed_answer field!
```

**Impact**: `AttributeError` crashes the logging, potentially losing the response entirely.

**Fix**: Store original draft separately and access correct fields:

```python
properties = {
    "query": query,
    "draft_answer": original_draft,  # Pass as parameter
    "skeptic_critique": last_audit.reasoning if last_audit else "",
    # ...
}
```

---

## P1: High-Priority Bugs

### P1-1: `model_override` Parameter is Ignored

**Location**: Line 129-133 in verified.py, Lines 355-502 in base.py

**Problem**: `_call_llm` accepts `model_override` parameter but never uses it:

```python
# In verified.py:
response_str = await self._call_llm(
    prompt,
    model_override=self.skeptic_model,  # Passed but ignored!
    temperature=0.1
)

# In base.py _call_llm - model_override never used:
async def _call_llm(self, prompt: str, model_override: str=None, **kwargs) -> str:
    # ...
    if self.llm_backend == "ollama":
        payload = {
            "model": self.ollama_model,  # Always uses default, ignores override!
            # ...
        }
```

**Impact**: Skeptic model configuration has no effect; same model used for all calls.

**Fix**: Implement model_override in base.py:

```python
if self.llm_backend == "ollama":
    model_to_use = model_override if model_override else self.ollama_model
    payload = {
        "model": model_to_use,
        # ...
    }
```

---

### P1-2: `was_refined` Logic is Incorrect

**Location**: Line 271

**Problem**: `was_refined` checks if `attempt_count > 0`, but `add_audit()` increments the counter on the **first** audit:

```python
# In VerificationState:
def add_audit(self, audit: SkepticAuditResult):
    self.history.append(audit)
    self.attempt_count += 1  # First audit sets count to 1

# In _log_debate:
"was_refined": state.attempt_count > 0,  # Always True after first audit!
```

**Impact**: All answers incorrectly logged as "refined" even if verified on first attempt.

**Fix**: Check history length or add explicit refined flag:

```python
"was_refined": len(state.history) > 1,  # True only if multiple audits
```

---

### P1-3: Fragile JSON Parsing

**Location**: Lines 136-144

**Problem**: JSON extraction uses simple string find/rfind which can fail on:
- Nested JSON structures in explanations
- Multiple JSON objects in response
- JSON with escaped braces in strings
- Markdown code blocks with different formatting

```python
clean_json = response_str.replace("```json", "").replace("```", "").strip()
if "{" in clean_json and "}" in clean_json:
    start = clean_json.find("{")
    end = clean_json.rfind("}") + 1
    clean_json = clean_json[start:end]
```

**Impact**: Parse failures trigger P0-3 (defaults to verified=True).

**Fix**: Use more robust JSON extraction:

```python
import re

def _extract_json(self, text: str) -> dict | None:
    """Extract JSON from LLM response with multiple fallback strategies."""
    # Strategy 1: Direct parse
    try:
        return json.loads(text.strip())
    except json.JSONDecodeError:
        pass

    # Strategy 2: Extract from markdown code block
    code_block_match = re.search(r'```(?:json)?\s*([\s\S]*?)\s*```', text)
    if code_block_match:
        try:
            return json.loads(code_block_match.group(1))
        except json.JSONDecodeError:
            pass

    # Strategy 3: Find outermost braces (handles nested)
    depth = 0
    start = None
    for i, char in enumerate(text):
        if char == '{':
            if depth == 0:
                start = i
            depth += 1
        elif char == '}':
            depth -= 1
            if depth == 0 and start is not None:
                try:
                    return json.loads(text[start:i+1])
                except json.JSONDecodeError:
                    start = None

    return None
```

---

### P1-4: Missing `retrieve_only()` Implementation

**Location**: N/A (missing method)

**Problem**: `VerifiedRAGPipeline` does not implement `retrieve_only()`, which is required for the streaming integration in Phase 1.

**Impact**:
- Calling `/rag/retrieve/verified` returns 501 Not Implemented
- Go orchestrator cannot use verified pipeline for streaming

**Fix**: Implement `retrieve_only()` by delegating to parent:

```python
async def retrieve_only(
    self,
    query: str,
    session_id: str | None = None,
    strict_mode: bool = True,
    max_chunks: int = 5
) -> tuple[list[dict], str, bool]:
    """
    Retrieval-only mode for the verified pipeline.

    For retrieval-only, we use the parent RerankingPipeline's implementation
    since the verification loop requires LLM generation.
    """
    return await super().retrieve_only(query, session_id, strict_mode, max_chunks)
```

---

### P1-5: Inconsistent Metadata Access Pattern

**Location**: Lines 172-173, 237-239, 257-258

**Problem**: Multiple patterns used to access `rerank_score`:

```python
# Pattern 1: hasattr check
if d.get("metadata") and hasattr(d["metadata"], "rerank_score")

# Pattern 2: Direct access (would fail if not present)
d["metadata"].rerank_score
```

**Impact**: Potential `AttributeError` if metadata structure varies.

**Fix**: Create a helper method for consistent access:

```python
def _get_rerank_score(self, doc: dict) -> float | None:
    """Safely extract rerank score from document metadata."""
    metadata = doc.get("metadata")
    if metadata is None:
        return None
    return getattr(metadata, "rerank_score", None)
```

---

## P2: Medium-Priority Issues

### P2-1: Duplicated Retrieval Logic

**Location**: Lines 162-186

**Problem**: The `run()` method duplicates retrieval, reranking, and filtering logic that exists in `RerankingPipeline.run()` and `RerankingPipeline.retrieve_only()`.

```python
# Duplicated in verified.py:
query_vector = await self._get_embedding(query)
initial_docs = await self._search_weaviate_initial(query_vector, session_id)
context_docs = await self._rerank_docs(query, initial_docs)
# ... filtering logic ...
```

**Impact**:
- Code duplication increases maintenance burden
- Bug fixes must be applied in multiple places
- Risk of divergent behavior

**Fix**: Refactor to use parent's `retrieve_only()`:

```python
async def run(self, query: str, session_id: str | None = None,
              strict_mode: bool = True) -> tuple[str, list[dict]]:
    # Use parent's retrieval logic
    chunks, context_text, has_relevant = await super().retrieve_only(
        query, session_id, strict_mode, max_chunks=self.top_k_final
    )

    if not has_relevant and strict_mode:
        return NO_RELEVANT_DOCS_MESSAGE, []

    # Build context_docs from chunks for downstream use
    context_docs = [{"properties": {"content": c["content"], "source": c["source"]}}
                    for c in chunks]

    # Continue with verification loop...
```

---

### P2-2: Off-by-One in Verification Loop

**Location**: Lines 198, 219

**Problem**: The loop condition and inner check are both `<= MAX_VERIFICATION_RETRIES`:

```python
MAX_VERIFICATION_RETRIES = 2

while state.attempt_count <= MAX_VERIFICATION_RETRIES:  # 0, 1, 2 = 3 iterations
    # ...
    if state.attempt_count <= MAX_VERIFICATION_RETRIES:  # Always true inside loop
        # Refine...
```

**Impact**:
- The inner `if` is redundant (always true)
- Naming is confusing: "2 retries" actually means 3 total attempts

**Fix**: Clarify semantics:

```python
MAX_VERIFICATION_ATTEMPTS = 3  # Total attempts, not retries

while state.attempt_count < MAX_VERIFICATION_ATTEMPTS:
    # Run skeptic
    audit_result = await self._execute_skeptic_scan(audit_req)
    state.add_audit(audit_result)

    if audit_result.is_verified:
        state.mark_verified()
        break

    # Refine if more attempts remain
    if state.attempt_count < MAX_VERIFICATION_ATTEMPTS:
        # Refine...
```

---

### P2-3: Original Draft Not Passed to Logging

**Location**: Lines 191, 250

**Problem**: `original_draft` is captured but never passed to `_log_debate()`:

```python
original_draft = initial_answer  # Captured but not used
# ...
await self._log_debate(query, state, final_output, session_id)  # No original_draft param
```

**Fix**: Pass original draft:

```python
await self._log_debate(query, original_draft, state, final_output, session_id)

async def _log_debate(self, query: str, original_draft: str,
                      state: VerificationState, final_answer: str, session_id: str):
    properties = {
        "draft_answer": original_draft,
        # ...
    }
```

---

### P2-4: Emoji in Production Logging

**Location**: Line 281

**Problem**: Log message contains emoji which can cause encoding issues:

```python
logger.info("✅ Debate log saved to Weaviate.")
```

**Impact**:
- Log parsing tools may break
- Encoding issues in certain terminals/log aggregators
- Inconsistent with rest of codebase

**Fix**: Remove emoji:

```python
logger.info("Debate log saved to Weaviate successfully")
```

---

## P3: Low-Priority Improvements

### P3-1: Exception Handler Too Broad

**Location**: Line 146

**Problem**: Catches `Exception` which masks specific errors:

```python
except Exception as e:  # Too broad
```

**Fix**: Catch specific exceptions:

```python
except (json.JSONDecodeError, KeyError, TypeError) as e:
```

---

### P3-2: Newline Replacement in Evidence

**Location**: Line 49

**Problem**: Replaces all newlines with spaces, which can destroy code formatting:

```python
content = doc.get('content', '').replace("\n", " ")
```

**Impact**: Code snippets become unreadable.

**Fix**: Preserve formatting but limit length:

```python
content = doc.get('content', '')[:2000]  # Limit length, preserve formatting
```

---

### P3-3: Synchronous Weaviate Insert in Async Method

**Location**: Lines 276-279

**Problem**: `verification_logs.data.insert()` may be blocking in an async context.

**Fix**: Use `asyncio.to_thread` or async Weaviate client if available:

```python
await asyncio.to_thread(
    verification_logs.data.insert,
    properties=properties
)
```

---

### P3-4: Magic Strings for Collection Name

**Location**: Line 278

**Problem**: Hardcoded collection name:

```python
verification_logs = self.weaviate_client.collections.get("VerificationLog")
```

**Fix**: Use constant:

```python
VERIFICATION_LOG_COLLECTION = "VerificationLog"
# ...
verification_logs = self.weaviate_client.collections.get(VERIFICATION_LOG_COLLECTION)
```

---

## Implementation Plan

### Phase 1: Critical Fixes (P0)

1. **Fix `_build_refiner_prompt()`** - Write proper refinement prompt
2. **Fix field references** - Use correct `RefinerRequest` field names
3. **Fix error handling** - Default to `is_verified=False`
4. **Fix logging** - Pass original_draft as parameter

### Phase 2: High-Priority Fixes (P1)

5. **Implement `model_override`** in base.py
6. **Fix `was_refined` logic**
7. **Improve JSON parsing** with robust extractor
8. **Add `retrieve_only()`** method
9. **Create metadata access helper**

### Phase 3: Refactoring (P2)

10. **Refactor to use parent retrieval**
11. **Fix loop semantics**
12. **Pass original_draft to logging**
13. **Remove emoji from logs**

### Phase 4: Polish (P3)

14. **Narrow exception handling**
15. **Preserve content formatting**
16. **Make Weaviate insert async**
17. **Extract collection name constant**

---

## Test Cases Required

```python
# P0 Tests
test_refiner_produces_prose_not_json()
test_refiner_uses_correct_field_names()
test_json_parse_failure_marks_unverified()
test_log_debate_handles_all_fields()

# P1 Tests
test_skeptic_model_override_is_applied()
test_was_refined_false_on_first_pass()
test_json_extraction_handles_nested()
test_json_extraction_handles_markdown()
test_retrieve_only_delegates_to_parent()
test_metadata_access_handles_missing()

# P2 Tests
test_run_uses_parent_retrieval()
test_verification_loop_count_matches_config()
test_original_draft_logged_correctly()

# Integration Tests
test_verified_pipeline_end_to_end()
test_verified_pipeline_with_hallucinations()
test_verified_pipeline_all_retries_exhausted()
```

---

## Phase 5: Skeptic/Optimist Enhancements (P4)

**Status**: PLANNED
**Added**: 2026-01-11

These improvements enhance the quality and robustness of the skeptic/optimist debate pattern.

### P4-1: Add Evidence to Refiner Prompt (HIGH)

**Problem**: Refiner only sees hallucination list, not the actual evidence. It's "blind" to what's verifiable.

**Current**:
```python
return f"""...
HALLUCINATIONS TO REMOVE:
{hallucination_list}
..."""
```

**Fix**: Include evidence so refiner can verify what it keeps:
```python
return f"""...
AVAILABLE EVIDENCE (use this to verify what you keep):
{evidence_str}

HALLUCINATIONS TO REMOVE:
{hallucination_list}
..."""
```

**Impact**: Refiner can make informed decisions about borderline claims.

---

### P4-2: Temperature Differentiation Per Role (HIGH)

**Problem**: All roles use same temperature. Skeptic needs consistency; optimist needs creativity.

**Fix**: Add role-specific temperature params:

| Role | Temperature | Rationale |
|------|-------------|-----------|
| Optimist | 0.5-0.7 | Creative but grounded |
| Skeptic | 0.1-0.3 | Consistent, deterministic |
| Refiner | 0.3-0.5 | Careful rewrites |

```python
# In __init__
self.optimist_temperature = config.get("optimist_temperature", 0.6)
self.skeptic_temperature = config.get("skeptic_temperature", 0.2)
self.refiner_temperature = config.get("refiner_temperature", 0.4)

# In _execute_skeptic_scan
await self._call_llm(prompt, temperature=self.skeptic_temperature)
```

**Requires**: Extend `_call_llm()` to accept temperature override.

---

### P4-3: Enhanced Optimist Prompt (MEDIUM)

**Problem**: Optimist doesn't know it will be audited. Generic prompt doesn't encourage citation.

**Current**:
```
Answer the user's question based *only* on the provided context.
```

**Fix**: Audit-aware prompt:
```python
OPTIMIST_PROMPT = """You are generating a DRAFT answer that will be fact-checked by a skeptic.

GUIDELINES:
1. For each claim, ensure you can point to specific evidence in the context
2. Cite source numbers when making factual claims: "According to [Source 2]..."
3. If uncertain, qualify your statement: "The documents suggest..."
4. Avoid making claims that combine/infer across multiple sources
5. If the context doesn't fully answer the question, acknowledge limitations

Context:
{context}

Question: {query}

Draft Answer (will be verified):"""
```

---

### P4-4: Few-Shot Examples for Skeptic (MEDIUM)

**Problem**: Skeptic prompt explains rules but no demonstrations. LLMs perform better with examples.

**Fix**: Add 2-3 calibration examples:
```python
SKEPTIC_FEW_SHOT = """
EXAMPLE 1 - Hallucination (fabricated statistic):
Query: "What is Paris known for?"
Answer: "Paris has a population of 2.1 million and is known for the Eiffel Tower."
Evidence: [Source 0]: "Paris is the capital of France, famous for the Eiffel Tower."
Verdict: {"is_verified": false, "reasoning": "Population claim (2.1M) has no support in evidence", "hallucinations": ["population of 2.1 million"], "missing_evidence": ["population statistics"]}

EXAMPLE 2 - Verified (all claims supported):
Query: "What is Paris known for?"
Answer: "Paris is the capital of France and is famous for the Eiffel Tower."
Evidence: [Source 0]: "Paris is the capital of France, famous for the Eiffel Tower."
Verdict: {"is_verified": true, "reasoning": "Both claims directly supported by Source 0", "hallucinations": [], "missing_evidence": []}

EXAMPLE 3 - Hallucination (unsupported inference):
Query: "Is the Eiffel Tower popular?"
Answer: "The Eiffel Tower is the most visited monument in the world."
Evidence: [Source 0]: "The Eiffel Tower is a famous landmark in Paris."
Verdict: {"is_verified": false, "reasoning": "'Most visited' is an inference not stated in evidence", "hallucinations": ["most visited monument in the world"], "missing_evidence": ["visitor statistics"]}
"""
```

---

### P4-5: Source Metadata in Evidence (LOW)

**Problem**: Evidence shows content but not source quality/metadata.

**Current**:
```
[Source 0]:
content here
```

**Fix**: Include relevance scores and source info:
```python
def _format_evidence(self, context_docs: list[dict]) -> str:
    evidence_parts = []
    for i, doc in enumerate(context_docs):
        content = doc.get('content', '')[:MAX_EVIDENCE_CONTENT_LENGTH]
        source = doc.get('properties', {}).get('source', 'Unknown')
        score = self._get_rerank_score(doc)
        score_str = f", relevance: {score:.2f}" if score else ""

        evidence_parts.append(f"[Source {i}] ({source}{score_str}):\n{content}")

    return "\n\n---\n\n".join(evidence_parts)
```

**Impact**: Helps skeptic cite specific sources; shows which sources are most relevant.

---

### P4-6: Confidence Scoring for Hallucinations (LOW)

**Problem**: Binary hallucination list doesn't indicate severity/confidence.

**Current**:
```json
{"hallucinations": ["claim 1", "claim 2"]}
```

**Fix**: Structured hallucination objects:
```json
{
    "hallucinations": [
        {"claim": "population is 2.1 million", "severity": "critical", "confidence": 0.95, "type": "fabrication"},
        {"claim": "built in 1889", "severity": "minor", "confidence": 0.6, "type": "unverified_detail"}
    ]
}
```

**Hallucination types**:
- `fabrication`: Completely made up
- `exaggeration`: Overstated claim
- `misattribution`: Wrong source cited
- `unverified_detail`: Plausible but not in evidence
- `inference`: Connected dots not explicitly stated

**Impact**: Refiner can prioritize; low-confidence items might be qualified rather than removed.

---

### P4-7: Semantic Similarity for Stall Detection (LOW)

**Problem**: Current stall detection uses character-level prefix matching. Misses semantic equivalence.

**Current**:
```python
# Count matching prefix characters
for i in range(shorter):
    if old_normalized[i] == new_normalized[i]:
        matching += 1
```

**Fix**: Add embedding-based similarity as secondary check:
```python
def _is_refinement_stalled(self, old_answer: str, new_answer: str) -> bool:
    # Fast path: exact match
    if old_answer.strip().lower() == new_answer.strip().lower():
        return True

    # Character-level check (existing)
    if self._char_similarity(old_answer, new_answer) > 0.95:
        return True

    # Semantic similarity check (slower, more accurate)
    old_embedding = await self._get_embedding(old_answer[:500])
    new_embedding = await self._get_embedding(new_answer[:500])
    cosine_sim = self._cosine_similarity(old_embedding, new_embedding)

    if cosine_sim > 0.98:
        logger.debug(f"Semantic stall detected: {cosine_sim:.3f} similarity")
        return True

    return False
```

**Trade-off**: Adds latency (embedding call). Could make configurable.

---

## Phase 6: Advanced Improvements (P5)

These are deeper architectural enhancements for future consideration.

### P5-1: Explicit Claim Extraction

**Concept**: Before verification, explicitly extract claims from the answer as a separate step.

```
Answer: "Paris, the capital of France, has 2M people and the Eiffel Tower."
         ↓
Claims: ["Paris is the capital of France", "Paris has 2M people", "Paris has the Eiffel Tower"]
         ↓
Verify each claim individually
```

**Benefits**:
- More granular verification
- Easier to track which specific claims are problematic
- Refiner knows exactly which sentences to modify

---

### P5-2: Contradiction Detection

**Problem**: Skeptic checks if claims are *unsupported* but not if they *contradict* evidence.

**Example**:
- Evidence: "The project was completed in 2020"
- Answer: "The project finished in 2019"
- Current skeptic might miss this (both mention completion)

**Fix**: Add contradiction check to skeptic prompt:
```
AUDIT PROCESS:
Step 1: Break answer into claims
Step 2: For each claim, check:
   a) Is it SUPPORTED by evidence? (direct match)
   b) Does it CONTRADICT evidence? (opposite claim)
   c) Is it UNVERIFIABLE? (no relevant evidence)
Step 3: Contradictions are worse than unsupported claims
```

---

### P5-3: Source Triangulation

**Concept**: For critical claims, require multiple source confirmation.

```python
TRIANGULATION_PROMPT = """
For CRITICAL claims (numbers, dates, names), check if multiple sources agree.
If only one source supports a critical claim, mark confidence as "low".
If multiple sources agree, mark confidence as "high".
"""
```

---

### P5-4: Iterative Evidence Retrieval

**Problem**: If skeptic finds hallucination, we refine but don't retrieve more evidence.

**Enhancement**: On hallucination detection, do targeted retrieval:
```python
if not audit_result.is_verified:
    # Get more evidence for the specific hallucinated claims
    for hallucination in audit_result.hallucinations:
        additional_docs = await self._search_weaviate_initial(
            await self._get_embedding(hallucination),
            session_id
        )
        context_docs.extend(additional_docs)
```

---

### P5-5: Partial Verification Status

**Problem**: Binary `is_verified` loses nuance.

**Enhancement**: Return verification breakdown:
```json
{
    "status": "partially_verified",
    "verified_claims": ["Paris is the capital of France"],
    "unverified_claims": ["population is 2.1 million"],
    "verification_ratio": 0.5,
    "recommendation": "refine"
}
```

---

### P5-6: Prompt Versioning & A/B Testing

**Problem**: No way to track which prompt version produced which results.

**Fix**: Add prompt versioning:
```python
SKEPTIC_PROMPT_VERSION = "v2.1"
REFINER_PROMPT_VERSION = "v1.3"

# In span attributes
span.set_attribute("prompt.skeptic_version", SKEPTIC_PROMPT_VERSION)
span.set_attribute("prompt.refiner_version", REFINER_PROMPT_VERSION)

# In debate log
properties["skeptic_prompt_version"] = SKEPTIC_PROMPT_VERSION
```

Enables:
- A/B testing different prompts
- Rollback if new prompt performs worse
- Correlation between prompt version and verification rates

---

### P5-7: Human Feedback Loop

**Concept**: Allow marking skeptic verdicts as false positive/negative.

```python
# New Weaviate collection
VERIFICATION_FEEDBACK_COLLECTION = "VerificationFeedback"

properties = {
    "trace_id": trace_id,
    "feedback_type": "false_positive",  # skeptic wrongly flagged as hallucination
    "claim": "the specific claim",
    "user_correction": "actually this was correct because...",
    "timestamp": ...
}
```

**Benefits**:
- Train better prompts based on mistakes
- Track skeptic precision/recall over time
- Build evaluation dataset

---

### P5-8: Context Window Management

**Problem**: Long evidence + long answers could overflow context window.

**Fix**: Dynamic truncation based on model context limit:
```python
def _fit_to_context(self, prompt: str, max_tokens: int = 4096) -> str:
    estimated_tokens = len(prompt) // 4  # rough estimate
    if estimated_tokens > max_tokens * 0.8:
        # Truncate evidence first, then answer
        ...
```

---

### P5-9: Verification Metrics Dashboard

**Concept**: Track aggregate metrics for monitoring:

- Verification rate (% verified on first attempt)
- Average attempts per query
- Common hallucination types
- Stall rate
- Refinement success rate

```python
# Emit metrics via OTEL
meter = metrics.get_meter("aleutian.rag.verified")
verification_counter = meter.create_counter("verification.attempts")
hallucination_histogram = meter.create_histogram("hallucinations.count")
```

---

## Phase 7: Debate Progress Streaming (P6) - HIGH PRIORITY

**Status**: PLANNED - IMPLEMENT NEXT
**Added**: 2026-01-11
**Priority**: **HIGHEST** - User experience critical

This feature provides real-time feedback to users during the skeptic/optimist verification loop, showing the debate progress as it happens.

### Implementation Requirements

**Go Code Style (MANDATORY)**:
1. **Interfaces first** - Define contracts before implementations
2. **Structs second** - Data structures after interfaces
3. **Methods with pointer receivers** - `func (s *SomeType) MethodName() { ... }`
4. **Extensive GoDoc documentation** for every exported type/method:
   - Description: What it does
   - Inputs: Parameters and their constraints
   - Outputs: Return values and error conditions
   - Examples: Concrete usage examples
   - Limitations: Known constraints or edge cases
   - Assumptions: What system state is assumed
5. **Single Responsibility Principle** - Each method does ONE thing (repeat code if needed)
6. **Unit tests must mock all external services** - No real network calls
7. **Tests must run fast** - Target <1s per test file
8. **Excellent test coverage** - Near 100% for logic paths

**Process Requirements**:
- Implement ONE file at a time
- Ask for review after each file
- Surface ALL assumptions explicitly
- Do NOT proceed without explicit approval

**Configuration**:
- **Default verbosity: 2** (detailed) for debugging/development
- Will dial back to 1 (summary) for release

### P6-1: Problem Statement

**Current State**: When using `pipeline=verified`, users see no feedback during the debate loop. The CLI shows "Searching knowledge base..." then nothing for potentially 30-60 seconds while the skeptic/optimist debate runs.

**User Experience**:
```
Current:
> What is George Washington known for?
Searching knowledge base...
[... 45 seconds of silence ...]
George Washington was the first president...

Desired:
> What is George Washington known for?
Searching knowledge base...
Generating initial draft...
Skeptic auditing claims...
Found 2 unsupported claims, refining...
Verification attempt 2/3...
Answer verified ✓
George Washington was the first president...
```

---

### P6-2: Verbosity Levels

Three verbosity levels for different user needs:

**Level 0 (Silent)**: No debate progress shown
```
George Washington was the first president...
```

**Level 1 (Summary)**: Status messages only
```
Generating initial draft...
Skeptic auditing claims...
Refining answer...
Answer verified ✓

George Washington was the first president...
```

**Level 2 (Detailed)** [DEFAULT]: Full debug information
```
Generating initial draft... (temp=0.6)
Skeptic auditing 5 claims against 3 sources...
├─ Hallucinations found: 2
│  ├─ "most popular programming language" (no evidence)
│  └─ "used by 10 million developers" (no evidence)
├─ Sources used: [Source 0, Source 2]
└─ Verdict: UNVERIFIED
Refining answer... (temp=0.4)
├─ Removing 2 unsupported claims
├─ Keeping 3 verified claims
└─ New answer: 847 chars
Verification attempt 2/3...
Skeptic auditing 3 claims...
├─ All claims supported
└─ Verdict: VERIFIED ✓

💡 For full trace details, view in Jaeger/Phoenix: http://localhost:16686/trace/{trace_id}

George Washington was the first president...
```

**OTel Integration**: At verbosity 2, the CLI will display a link to the OpenTelemetry trace in Jaeger/Phoenix where users can see:
- Complete span hierarchy for the debate
- LLM prompt/response content (truncated in spans)
- Timing breakdown for each stage
- All hallucination details and evidence used

This enables deep debugging without cluttering the CLI output.

---

### P6-3: Architecture

```
                              SSE Events
CLI ←──────────────────────────────────────────── Go Orchestrator
  │                                                      │
  │ verbosity=1: summary only                            │
  │ verbosity=2: detailed debug                          │
  │                                                      │
  └──────────────────────────────────────────────────────┘
                              │
                              │ HTTP SSE Stream
                              ▼
                    Python RAG Engine
                    /rag/verified/stream
                              │
                              │ Progress Callbacks
                              ▼
                    VerifiedRAGPipeline
                    ├─ on_draft_start()
                    ├─ on_draft_complete()
                    ├─ on_skeptic_start()
                    ├─ on_skeptic_verdict()
                    ├─ on_refine_start()
                    ├─ on_refine_complete()
                    └─ on_verified()
```

---

### P6-4: Event Types

```python
class DebateProgressEvent(BaseModel):
    """Progress event emitted during verified pipeline execution."""
    event_type: str  # "status", "debate_progress", "error"
    stage: str       # "draft", "skeptic", "refine", "verified", "failed"
    attempt: int
    max_attempts: int
    message: str     # Human-readable summary

    # Detailed info (verbosity=2 only)
    details: Optional[DebateProgressDetails] = None


class DebateProgressDetails(BaseModel):
    """Detailed debug info for verbosity=2."""
    claims_count: Optional[int] = None
    hallucinations: Optional[list[str]] = None
    sources_used: Optional[list[int]] = None
    verdict: Optional[str] = None  # "verified", "unverified", "error"
    temperature: Optional[float] = None
    answer_length: Optional[int] = None
```

---

### P6-5: Implementation Components

#### 5a. Python: Progress Callback System

```python
# In verified.py
from typing import Callable, Optional
from dataclasses import dataclass

@dataclass
class ProgressEvent:
    stage: str
    attempt: int
    max_attempts: int
    message: str
    details: Optional[dict] = None

ProgressCallback = Callable[[ProgressEvent], None]


class VerifiedRAGPipeline:
    async def run_with_progress(
        self,
        query: str,
        session_id: str | None = None,
        strict_mode: bool = True,
        progress_callback: ProgressCallback | None = None,
        temperature_overrides: dict | None = None
    ) -> tuple[str, list[dict]]:
        """Execute verified pipeline with progress callbacks."""

        def emit(stage: str, message: str, details: dict = None):
            if progress_callback:
                progress_callback(ProgressEvent(
                    stage=stage,
                    attempt=state.attempt_count,
                    max_attempts=self.max_verification_attempts,
                    message=message,
                    details=details
                ))

        # ... retrieval ...

        emit("draft", "Generating initial draft...")
        initial_answer = await self._call_llm(...)
        emit("draft", "Draft complete", {"answer_length": len(initial_answer)})

        while not state.is_final_verified and ...:
            emit("skeptic", f"Skeptic auditing claims (attempt {state.attempt_count + 1}/{self.max_verification_attempts})...")

            audit_result = await self._execute_skeptic_scan(...)

            emit("skeptic", "Audit complete", {
                "verdict": "verified" if audit_result.is_verified else "unverified",
                "hallucinations": audit_result.hallucinations,
                "claims_count": len(audit_result.hallucinations) + len(audit_result.missing_evidence)
            })

            if not audit_result.is_verified:
                emit("refine", f"Found {len(audit_result.hallucinations)} unsupported claims, refining...")
                refined = await self._execute_refinement(...)
                emit("refine", "Refinement complete", {"answer_length": len(refined)})

        emit("verified" if state.is_final_verified else "failed",
             "Answer verified ✓" if state.is_final_verified else "Verification failed after max attempts")

        return final_answer, sources
```

#### 5b. Python: Streaming Endpoint

```python
# In server.py
from fastapi.responses import StreamingResponse
import asyncio

class VerifiedStreamRequest(BaseModel):
    query: str
    session_id: str | None = None
    strict_mode: bool = True
    temperature_overrides: TemperatureOverrides | None = None
    verbosity: int = 1  # 1=summary, 2=detailed


@app.post("/rag/verified/stream")
async def stream_verified_rag(request: VerifiedStreamRequest):
    """SSE endpoint for verified pipeline with progress events."""

    async def event_generator():
        queue = asyncio.Queue()

        def progress_callback(event: ProgressEvent):
            # Convert to SSE format
            data = {
                "type": "debate_progress",
                "stage": event.stage,
                "attempt": event.attempt,
                "max_attempts": event.max_attempts,
                "message": event.message,
            }
            if request.verbosity >= 2 and event.details:
                data["details"] = event.details

            asyncio.get_event_loop().call_soon_threadsafe(
                queue.put_nowait, f"data: {json.dumps(data)}\n\n"
            )

        # Start pipeline in background task
        pipeline = verified.VerifiedRAGPipeline(weaviate_client, pipeline_config)

        async def run_pipeline():
            try:
                answer, sources = await pipeline.run_with_progress(
                    request.query,
                    request.session_id,
                    request.strict_mode,
                    progress_callback=progress_callback,
                    temperature_overrides=request.temperature_overrides.model_dump() if request.temperature_overrides else None
                )
                # Send final result
                await queue.put(f"data: {json.dumps({'type': 'result', 'answer': answer, 'sources': sources})}\n\n")
                await queue.put(None)  # Signal completion
            except Exception as e:
                await queue.put(f"data: {json.dumps({'type': 'error', 'error': str(e)})}\n\n")
                await queue.put(None)

        asyncio.create_task(run_pipeline())

        # Yield events as they arrive
        while True:
            event = await queue.get()
            if event is None:
                break
            yield event

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"}
    )
```

#### 5c. Go: Handler for Verified Streaming

```go
// In handlers/chat_streaming.go

// HandleChatRAGVerifiedStream handles streaming for verified pipeline with debate progress.
func (h *streamingChatHandler) HandleChatRAGVerifiedStream(c *gin.Context) {
    // ... setup ...

    // Check if pipeline is "verified" - use specialized streaming
    if req.Pipeline == "verified" {
        h.streamVerifiedPipeline(ctx, c, &req, sseWriter)
        return
    }

    // ... existing flow for other pipelines ...
}

func (h *streamingChatHandler) streamVerifiedPipeline(
    ctx context.Context,
    c *gin.Context,
    req *datatypes.ChatRAGRequest,
    sseWriter SSEWriter,
) {
    // Build request for Python streaming endpoint
    streamReq := map[string]interface{}{
        "query":      req.Message,
        "session_id": req.SessionId,
        "strict_mode": req.StrictMode,
        "verbosity": 1,  // TODO: Get from request
    }
    if req.TemperatureOverrides != nil {
        streamReq["temperature_overrides"] = req.TemperatureOverrides.ToMap()
    }

    // Connect to Python SSE endpoint
    resp, err := h.connectToVerifiedStream(ctx, streamReq)
    if err != nil {
        sseWriter.WriteError("Failed to connect to verified pipeline")
        return
    }
    defer resp.Body.Close()

    // Forward events from Python to CLI
    reader := bufio.NewReader(resp.Body)
    var answer string
    var sources []datatypes.SourceInfo

    for {
        line, err := reader.ReadString('\n')
        if err == io.EOF {
            break
        }
        if err != nil {
            sseWriter.WriteError("Stream read error")
            return
        }

        if !strings.HasPrefix(line, "data: ") {
            continue
        }

        data := strings.TrimPrefix(line, "data: ")
        var event map[string]interface{}
        if err := json.Unmarshal([]byte(data), &event); err != nil {
            continue
        }

        eventType := event["type"].(string)

        switch eventType {
        case "debate_progress":
            // Forward as status event
            msg := event["message"].(string)
            sseWriter.WriteStatus(msg)

        case "result":
            answer = event["answer"].(string)
            // Parse sources...
            sseWriter.WriteSources(sources)

        case "error":
            sseWriter.WriteError(event["error"].(string))
            return
        }
    }

    // Stream the final answer token by token
    for _, token := range splitIntoTokens(answer) {
        sseWriter.WriteToken(token)
    }

    sseWriter.WriteDone(req.SessionId)
}
```

#### 5d. CLI: Verbosity Flag

```go
// In cmd/aleutian/cmd_chat.go

var chatCmd = &cobra.Command{
    Use:   "chat",
    Short: "Start an interactive chat session",
    Run: func(cmd *cobra.Command, args []string) {
        verbosity, _ := cmd.Flags().GetInt("verbosity")
        // Pass to streaming service config
    },
}

func init() {
    // Default to 2 (detailed) for development/debugging
    // Will dial back to 1 (summary) for release
    chatCmd.Flags().IntP("verbosity", "v", 2,
        "Verbosity for verified pipeline: 0=silent, 1=summary, 2=detailed (default)")
}
```

#### 5e. CLI: OTel Trace Link

After the debate completes at verbosity >= 2, display the trace link:

```go
// In streaming_service.go or chat_runner_rag.go

func (s *ragStreamingChatService) displayTraceLink(traceID string) {
    if s.verbosity >= 2 && traceID != "" {
        jaegerURL := os.Getenv("JAEGER_UI_URL")
        if jaegerURL == "" {
            jaegerURL = "http://localhost:16686"
        }
        fmt.Printf("\n💡 For full trace details, view in Jaeger/Phoenix: %s/trace/%s\n", jaegerURL, traceID)
    }
}
```

The trace ID is extracted from the SSE response headers or a dedicated event.

---

### P6-6: SSE Event Format

```
event: status
data: {"type": "debate_progress", "stage": "draft", "attempt": 0, "max_attempts": 3, "message": "Generating initial draft..."}

event: status
data: {"type": "debate_progress", "stage": "skeptic", "attempt": 1, "max_attempts": 3, "message": "Skeptic auditing claims...", "details": {"claims_count": 5}}

event: status
data: {"type": "debate_progress", "stage": "skeptic", "attempt": 1, "max_attempts": 3, "message": "Audit complete", "details": {"verdict": "unverified", "hallucinations": ["claim 1", "claim 2"]}}

event: status
data: {"type": "debate_progress", "stage": "refine", "attempt": 1, "max_attempts": 3, "message": "Found 2 unsupported claims, refining..."}

event: status
data: {"type": "debate_progress", "stage": "verified", "attempt": 2, "max_attempts": 3, "message": "Answer verified ✓"}

event: sources
data: {"type": "sources", "sources": [...]}

event: token
data: {"type": "token", "content": "George"}

event: done
data: {"type": "done", "session_id": "..."}
```

---

### P6-7: Test Plan

```python
# Python tests
test_verified_stream_emits_draft_event()
test_verified_stream_emits_skeptic_events()
test_verified_stream_emits_refine_events()
test_verified_stream_emits_verified_event()
test_verified_stream_emits_failed_event_on_max_attempts()
test_verified_stream_includes_details_at_verbosity_2()
test_verified_stream_handles_error_gracefully()
test_verified_stream_respects_temperature_overrides()
```

```go
// Go tests
TestVerifiedStreamForwardsProgressEvents()
TestVerifiedStreamForwardsSources()
TestVerifiedStreamStreamsAnswer()
TestVerifiedStreamHandlesError()
TestVerifiedStreamRespectsVerbosity()
```

---

### P6-8: Files to Modify

| Layer | File | Changes |
|-------|------|---------|
| Python | `pipelines/verified.py` | Add `run_with_progress()`, progress callbacks |
| Python | `server.py` | Add `/rag/verified/stream` endpoint |
| Python | `datatypes/verified.py` | Add `ProgressEvent`, `DebateProgressDetails` |
| Go | `handlers/chat_streaming.go` | Add `streamVerifiedPipeline()` |
| Go | `datatypes/rag.go` | Add `DebateProgressEvent` type |
| Go | `cmd/aleutian/cmd_chat.go` | Add `--verbosity` flag |
| Go | `cmd/aleutian/streaming_service.go` | Pass verbosity to request |

---

## Phase 8: Conversation History Support (P7) - IN PROGRESS

**Status**: 🔄 IN PROGRESS
**Added**: 2026-01-12
**Priority**: **HIGH** - Breaks "chat" UX expectation

### P7-1: Problem Statement

Follow-up queries fail in verified pipeline because it doesn't include conversation history:

```
User: "Tell me about Chrysler and how it was formed"
Assistant: [Good answer with sources from documents]

User: "Give me a longer explanation"
Assistant: "No relevant documents found"  ← BUG: Lost context!
```

The query "Give me a longer explanation" is semantically meaningless without knowing the previous question was about Chrysler. The regular RAG pipeline handles this correctly, but verified doesn't.

**User Impact**: Users expect "chat" to maintain context. This breaks that fundamental expectation.

---

### P7-2: Root Cause Analysis

**Regular RAG pipeline (works):**
```
1. Go: loadSessionHistory() → Weaviate Conversation class
2. Go: buildRAGMessagesWithHistory() → [system, user1, asst1, user2, asst2, ...]
3. Go: LLM sees full context → Understands "longer explanation" = more about Chrysler
```

**Verified pipeline (broken):**
```
1. Go: Sends query directly to Python /rag/verified/stream
2. Python: Builds prompt WITHOUT conversation history
3. Python: LLM sees isolated query "Give me a longer explanation"
4. Python: Retrieval finds nothing → "No relevant documents found"
```

---

### P7-3: Solution Architecture

```
CURRENT FLOW (BROKEN):
Go Handler ──query──▶ Python Verified ──isolated query──▶ Retrieval fails
                                ↑
                    No conversation context

NEW FLOW:
Go Handler ──loadSessionHistory──▶ Weaviate
     │
     └──query + history──▶ Python Verified ──contextualized query──▶ Retrieval succeeds
                                    │
                                    └── Optimist prompt includes history
```

---

### P7-4: Files to Modify

| File | Changes |
|------|---------|
| `services/orchestrator/handlers/chat_streaming.go` | Load history in `HandleVerifiedRAGStream` |
| `services/rag_engine/server.py` | Add `history` field to `VerifiedRAGRequest` |
| `services/rag_engine/pipelines/verified.py` | Include history in Optimist prompt |

---

### P7-5: Implementation Steps

#### Step 1: Go - Load Session History (chat_streaming.go)

In `HandleVerifiedRAGStream`, add session history loading (reuse existing `loadSessionHistory`):

```go
// Step 2.5: Load session history for session resume
var sessionHistory []ConversationTurn
if req.SessionId != "" {
    var historyErr error
    sessionHistory, historyErr = h.loadSessionHistory(ctx, req.SessionId)
    if historyErr != nil {
        slog.Warn("failed to load session history, continuing without history",
            "session_id", req.SessionId,
            "error", historyErr,
        )
    }
}
```

#### Step 2: Go - Pass History to Python

Modify the request body sent to Python `/rag/verified/stream`:

```go
// Build request with history
reqBody := map[string]interface{}{
    "query":       req.Message,
    "session_id":  sessionID,
    "strict_mode": req.StrictMode,
}

// Add conversation history if available
if len(sessionHistory) > 0 {
    historyTurns := make([]map[string]string, len(sessionHistory))
    for i, turn := range sessionHistory {
        historyTurns[i] = map[string]string{
            "question": turn.Question,
            "answer":   turn.Answer,
        }
    }
    reqBody["history"] = historyTurns
}
```

#### Step 3: Python - Accept History (server.py)

Add history field to `VerifiedRAGRequest`:

```python
class ConversationTurn(BaseModel):
    """A single Q&A exchange in conversation history."""
    question: str
    answer: str

class VerifiedRAGRequest(RAGEngineRequest):
    """Extended request model for verified pipeline."""
    temperature_overrides: TemperatureOverrides | None = None
    history: list[ConversationTurn] | None = None  # NEW: conversation history
```

#### Step 4: Python - Include History in Optimist Prompt (verified.py)

Modify `_build_optimist_prompt()` to include conversation context:

```python
def _build_optimist_prompt(
    self,
    query: str,
    context: str,
    history: list[dict] | None = None
) -> str:
    """Build the Optimist prompt with optional conversation history."""

    history_section = ""
    if history:
        history_section = "\n## Previous Conversation\n"
        for turn in history[-5:]:  # Last 5 turns to limit context
            history_section += f"User: {turn['question']}\n"
            history_section += f"Assistant: {turn['answer']}\n\n"

    return f"""You are generating a DRAFT answer that will be fact-checked by a skeptic.

## Context (Retrieved Documents)
{context}
{history_section}
## Current Question
{query}

## Guidelines
1. For each claim, ensure you can point to specific evidence in the context
2. Cite source numbers when making factual claims: "According to [Source 2]..."
3. If the question references previous conversation, use that context
4. If uncertain, qualify your statement: "The documents suggest..."

Draft Answer (will be verified):"""
```

---

### P7-6: Data Flow Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Go Orchestrator                             │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. HandleVerifiedRAGStream receives request                        │
│     ↓                                                               │
│  2. loadSessionHistory(sessionID) → Weaviate Conversation           │
│     ↓                                                               │
│  3. Build request body with history:                                │
│     {query, session_id, history: [{q1, a1}, {q2, a2}, ...]}        │
│     ↓                                                               │
│  4. POST /rag/verified/stream → Python                              │
│                                                                     │
└───────────────────────────────┬─────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         Python RAG Engine                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  5. VerifiedRAGRequest parses history                               │
│     ↓                                                               │
│  6. VerifiedPipeline.run_with_progress(query, history=history)      │
│     ↓                                                               │
│  7. Retrieval: Use query (history provides context for ambiguous    │
│                queries like "give me more details")                 │
│     ↓                                                               │
│  8. Optimist: Include history in prompt                             │
│     "Previous: User asked about Chrysler..."                        │
│     "Now: Give me a longer explanation"                             │
│     ↓                                                               │
│  9. Skeptic/Refiner: Operate on draft (no history needed directly)  │
│     ↓                                                               │
│  10. Stream final answer back to Go                                 │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

---

### P7-7: Design Decisions

1. **History format**: Simple `[{question, answer}]` array (matches existing types)
2. **History limit**: Last 5 turns in prompt (prevent context overflow), load up to 20 from Weaviate
3. **Query rewriting**: NOT implemented initially - let Optimist interpret context naturally
4. **Which roles get history**: Only Optimist - Skeptic/Refiner operate on the draft
5. **Error handling**: If history load fails, continue without it (graceful degradation)
6. **Weaviate schema**: Reuse existing `Conversation` class (already has session_id, question, answer)

---

### P7-8: Test Strategy

```python
# Python tests
test_verified_accepts_history_in_request()
test_verified_includes_history_in_optimist_prompt()
test_verified_limits_history_to_5_turns()
test_verified_works_without_history()
```

```go
// Go tests
TestVerifiedPipeline_LoadsSessionHistory()
TestVerifiedPipeline_PassesHistoryToPython()
TestVerifiedPipeline_FollowUpQueryWorks()
TestVerifiedPipeline_HistoryLoadFailure_Continues()
TestVerifiedPipeline_EmptyHistoryOK()
```

```bash
# E2E tests
E2E_VerifiedChat_FollowUpQueriesWork
E2E_VerifiedChat_ResumeSessionWithHistory
```

---

### P7-9: Acceptance Criteria

- [ ] `./aleutian chat --pipeline verified` → Ask question → "Give me more details" → Works
- [ ] `./aleutian chat --pipeline verified --resume <session_id>` → Follow-up queries work
- [ ] Session history is loaded from Weaviate Conversation class
- [ ] History is passed to Python in request body
- [ ] Optimist prompt includes conversation context
- [ ] Graceful degradation if history load fails
- [ ] History limited to last 5 turns in prompt

---

## Implementation Priority Matrix (UPDATED)

| ID | Enhancement | Impact | Effort | Priority |
|----|-------------|--------|--------|----------|
| P4-1 | Evidence in refiner | High | Low | **DONE** ✓ |
| P4-2 | Temperature per role | High | Low | **DONE** ✓ |
| P4-3 | Enhanced optimist prompt | Medium | Low | **DONE** ✓ |
| P4-4 | Few-shot skeptic examples | Medium | Medium | **DONE** ✓ |
| P6 | Debate Progress Streaming | HIGH | Medium | **DONE** ✓ |
| **P7** | **Conversation History** | **HIGH** | **Medium** | **🔄 IN PROGRESS** |
| P4-5 | Source metadata | Low | Low | After P7 |
| P4-6 | Confidence scoring | Medium | Medium | After P7 |
| P4-7 | Semantic stall detection | Low | Medium | Later |
| P5-1 | Claim extraction | High | High | Later |
| P5-2 | Contradiction detection | Medium | Low | After P7 |
| P5-3 | Source triangulation | Low | Medium | Later |
| P5-4 | Iterative retrieval | Medium | High | Later |
| P5-5 | Partial verification | Medium | Medium | Later |
| P5-6 | Prompt versioning | Medium | Low | After P7 |
| P5-7 | Human feedback loop | High | High | Later |
| P5-8 | Context management | Medium | Medium | Later |
| P5-9 | Metrics dashboard | Medium | Medium | Later |

---

## Log: 2026-Jan-12 — Plan Update & Comprehensive Review

**Author:** Claude Code Review
**Status:** Plan Revision

A comprehensive code review was conducted against the actual implementation to verify completion status of all phases. This revealed significantly more progress than previously tracked, plus several new gaps requiring attention.

---

### Completed Items Summary

#### Phase 1: Critical Fixes (P0) — ✅ 100% COMPLETE

| ID | Item | Evidence |
|----|------|----------|
| P0-1 | `_build_refiner_prompt()` distinct from skeptic | `verified.py:794-881` — Asks for prose, not JSON |
| P0-2 | Correct field references in RefinerRequest | Uses `req.draft_answer`, `req.audit_result`, `req.evidence_text` |
| P0-3 | Error handling defaults to `is_verified=False` | `verified.py:1135-1162` — Fail-safe behavior |
| P0-4 | `_log_debate()` handles fields correctly | `verified.py:1858-1961` — Receives `original_draft` as parameter |

#### Phase 2: High-Priority Fixes (P1) — ✅ 100% COMPLETE

| ID | Item | Evidence |
|----|------|----------|
| P1-1 | `model_override` in base.py | `base.py:426-512` — All backends support override |
| P1-2 | `was_refined` logic correct | `verified.py:1455` — Uses `len(state.history) > 1` |
| P1-3 | Robust JSON extraction | `verified.py:885-1016` — `_extract_json()` with 5 strategies |
| P1-4 | `retrieve_only()` method | `verified.py:1963-2048` — Delegates to parent |
| P1-5 | Consistent metadata access helpers | `verified.py:403-539` — `_get_rerank_score()`, `_get_distance()`, etc. |

#### Phase 3: Refactoring (P2) — ⚠️ 75% COMPLETE

| ID | Item | Status | Evidence |
|----|------|--------|----------|
| P2-1 | Refactor to use parent retrieval | ❌ NOT DONE | `verified.py:1296-1330` still duplicates logic |
| P2-2 | Fix loop semantics | ✅ Done | `verified.py:1361` uses `<` not `<=` |
| P2-3 | Pass original_draft to logging | ✅ Done | `verified.py:1465-1466` |
| P2-4 | Remove emoji from logs | ✅ Done | `verified.py:1949` |

#### Phase 4: Polish (P3) — ✅ 100% COMPLETE (Acceptable)

| ID | Item | Status | Notes |
|----|------|--------|-------|
| P3-1 | Narrow exception handling | ✅ Acceptable | Catch-all at line 1151 is intentional safety net |
| P3-2 | Preserve content formatting | ✅ Done | `verified.py:389-400` |
| P3-3 | Async Weaviate insert | ✅ Done | `verified.py:1940-1946` uses `asyncio.to_thread()` |
| P3-4 | Extract collection name constant | ✅ Done | `verified.py:66` |

#### Phase 5: Skeptic/Optimist Enhancements (P4) — ✅ P4-1 to P4-4 COMPLETE

| ID | Item | Status | Evidence |
|----|------|--------|----------|
| P4-1 | Evidence in refiner prompt | ✅ Done | `verified.py:848-855` |
| P4-2 | Temperature per role | ✅ Done | `verified.py:102-127, 213-226` — env + config + request-level |
| P4-3 | Enhanced optimist prompt | ✅ Done | `verified.py:626-702` — Strictness modes |
| P4-4 | Few-shot skeptic examples | ✅ Done | `verified.py:131-162, 250-327` — Hardcoded + YAML |
| P4-5 | Source metadata in evidence | ❌ Deferred | Low priority |
| P4-6 | Confidence scoring | ❌ Deferred | Medium priority |
| P4-7 | Semantic stall detection | ❌ Deferred | Low priority |

#### Phase 6: Debate Progress Streaming (P6) — ✅ 100% COMPLETE

| Component | Status | Evidence |
|-----------|--------|----------|
| Python `ProgressEvent` datatypes | ✅ Done | `datatypes/verified.py:23-289` |
| Python `run_with_progress()` | ✅ Done | `verified.py:1474-1856` (~380 lines) |
| Python `/rag/verified/stream` endpoint | ✅ Done | `server.py:426-591` |
| Go `HandleVerifiedRAGStream` handler | ✅ Done | `chat_streaming.go:1192-1368` |
| Go `streamFromVerifiedPipeline` | ✅ Done | `chat_streaming.go:1454-1502` |
| Go verbosity support (0/1/2) | ✅ Done | `chat_streaming.go:1370-1417` |
| Go event forwarding with filtering | ✅ Done | `chat_streaming.go:1544+` |
| Go turn persistence with hash chain | ✅ Done | `chat_streaming.go:1298-1364` |

#### Phase 7: Conversation History (P7) — ⏳ 20% COMPLETE

| Component | Status | Evidence |
|-----------|--------|----------|
| Go `loadSessionHistory()` | ✅ Done | `chat_streaming.go:2509+` |
| Go passes history in request | ❌ NOT DONE | `buildVerifiedStreamRequest()` missing history |
| Python accepts history field | ❌ NOT DONE | `VerifiedRAGRequest` missing field |
| Python uses history in pipeline | ❌ NOT DONE | `run_with_progress()` missing param |
| Optimist prompt includes history | ❌ NOT DONE | `_build_optimist_prompt()` missing history |

---

### New Issues Discovered

#### P8: Multi-Provider LLM Support — **CRITICAL**

**P8-1: Temperature Override Bug in Non-Ollama Backends**

**Location:** `base.py:486-493`

**Problem:** OpenAI and Local backends ignore the `temperature` parameter passed to `_call_llm()`:

```python
# CURRENT (BROKEN) - OpenAI backend
payload = {
    "model": effective_model,
    "messages": [{"role": "user", "content": prompt}],
    "temperature": self.default_llm_params["temperature"],  # BUG: ignores parameter!
    ...
}
```

**Impact:** Role-specific temperatures (P4-2) don't work with OpenAI/Local backends. Skeptic and Optimist use same temperature.

**Fix:**
```python
payload = {
    "model": effective_model,
    "messages": [{"role": "user", "content": prompt}],
    "temperature": generation_params["temperature"],  # Use the passed param
    ...
}
```

---

**P8-2: Gemini Backend Not Implemented**

**Location:** `base.py:426-544`

**Problem:** No Gemini/Google AI backend exists. The `_call_llm()` method supports:
- ✅ Ollama
- ✅ OpenAI
- ✅ Anthropic/Claude
- ✅ Local (llama.cpp)
- ❌ **Gemini (missing)**

**Impact:** Users cannot use Gemini models for verified pipeline.

**Fix:** Add Gemini backend to `_call_llm()`:
```python
elif self.llm_backend == "gemini":
    if not self.gemini_api_key:
        raise ValueError("Gemini API key secret not configured")
    api_url = "https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent"
    # ... implementation
```

---

**P8-3: Anthropic Temperature Handling**

**Location:** `base.py:452-458`

**Problem:** When thinking mode is enabled, temperature is hardcoded to `None`:
```python
if os.getenv("ENABLE_THINKING") == "true":
    payload["thinking"] = {...}
    payload["temperature"] = None  # Required for thinking, but loses override
```

**Impact:** Role-specific temperatures don't work with Claude thinking mode.

**Mitigation:** Document this limitation; thinking mode is opt-in.

---

#### P9: Test Coverage Gaps

**Current Tests:** Only 4 tests in `test_verified_pipeline.py`:
- `test_immediate_verification`
- `test_verification_refinement_loop`
- `test_max_retries_exceeded`
- `test_no_documents_found`

**Missing Test Categories:**

| Category | Missing Tests |
|----------|---------------|
| Streaming | `test_run_with_progress_emits_all_event_types()` |
| Streaming | `test_streaming_callback_error_doesnt_crash_pipeline()` |
| Streaming | `test_streaming_handles_llm_error_gracefully()` |
| History | `test_optimist_includes_conversation_history()` |
| History | `test_history_limited_to_5_turns()` |
| History | `test_empty_history_handled()` |
| Multi-provider | `test_skeptic_with_openai_backend()` |
| Multi-provider | `test_temperature_override_applied_openai()` |
| Temperature | `test_request_level_temperature_overrides()` |
| JSON parsing | `test_extract_json_all_strategies()` |
| JSON parsing | `test_extract_json_malformed_input()` |
| Few-shot | `test_skeptic_examples_from_yaml_file()` |
| Few-shot | `test_invalid_yaml_falls_back_to_defaults()` |

---

#### P10: Resilience & Production Readiness

**P10-1: No Retry Logic for Transient Failures**

**Problem:** Cloud APIs return 429 (rate limit) or 503 (overloaded). Current code fails immediately.

**Risk:** Single rate limit during skeptic audit fails entire pipeline.

**Fix:** Add exponential backoff:
```python
async def _call_llm_with_retry(self, prompt: str, max_retries: int = 3, **kwargs) -> str:
    for attempt in range(max_retries):
        try:
            return await self._call_llm(prompt, **kwargs)
        except (RateLimitError, ServiceUnavailableError) as e:
            if attempt == max_retries - 1:
                raise
            wait_time = 2 ** attempt  # 1s, 2s, 4s
            logger.warning(f"LLM call failed, retrying in {wait_time}s: {e}")
            await asyncio.sleep(wait_time)
```

---

**P10-2: No Context Window Management**

**Problem:** With history + evidence + few-shot examples, prompts can exceed model context limits.

**Risk:** Silent truncation or API errors with smaller models.

**Fix (P5-8):** Implement token counting and prioritized truncation:
1. Estimate tokens before call
2. If over limit, truncate in order: history → evidence → examples
3. Log warning when truncation occurs

---

#### P11: Configuration & UX

**P11-1: Mixed-Model Configuration Not Supported**

**Use Case:** User wants Claude for optimist (creative) but GPT-4 for skeptic (analytical).

**Current:** `skeptic_model` exists but assumes same backend.

**Future Enhancement:** Support per-role backend configuration:
```yaml
verified_pipeline:
  optimist:
    backend: anthropic
    model: claude-3-5-sonnet
    temperature: 0.7
  skeptic:
    backend: openai
    model: gpt-4o
    temperature: 0.2
```

---

### Remaining Work: Detailed Implementation Plan

#### Phase A: Critical Bug Fixes (Do First)

**A1: Fix Temperature Override Bug** — 15 min

File: `services/rag_engine/pipelines/base.py`

```python
# Line 486-493: Fix OpenAI backend
payload = {
    "model": effective_model,
    "messages": [{"role": "user", "content": prompt}],
    "temperature": generation_params["temperature"],  # FIXED
    "max_tokens": generation_params["max_tokens"],    # FIXED
    "top_p": generation_params["top_p"],              # FIXED
    "stop": generation_params["stop"]                 # FIXED
}

# Line 501-507: Fix Local backend
payload = {
    "prompt": prompt,
    "n_predict": generation_params["max_tokens"],     # FIXED
    "temperature": generation_params["temperature"],  # FIXED
    "top_k": generation_params["top_k"],              # FIXED
    "top_p": generation_params["top_p"],              # FIXED
    "stop": generation_params["stop"]                 # FIXED
}
```

**Tests Required:**
- `test_openai_backend_uses_temperature_override()`
- `test_local_backend_uses_temperature_override()`

---

#### Phase B: Complete Conversation History (P7)

**B1: Add History to Python Request Model** — 10 min

File: `services/rag_engine/server.py`

```python
class ConversationTurn(BaseModel):
    """A single Q&A exchange in conversation history."""
    question: str
    answer: str


class VerifiedRAGRequest(RAGEngineRequest):
    """Extended request model for verified pipeline."""
    temperature_overrides: TemperatureOverrides | None = None
    history: list[ConversationTurn] | None = None  # NEW
```

---

**B2: Add History Parameter to Pipeline Methods** — 15 min

File: `services/rag_engine/pipelines/verified.py`

```python
async def run(
    self,
    query: str,
    session_id: str | None = None,
    strict_mode: bool = True,
    temperature_overrides: dict | None = None,
    history: list[dict] | None = None,  # NEW
) -> tuple[str, list[dict]]:
    # ... existing code ...

    # Pass history to optimist prompt
    draft_prompt = self._build_optimist_prompt(query, context_props, history=history)


async def run_with_progress(
    self,
    query: str,
    progress_callback: Callable[[ProgressEvent], Awaitable[None]],
    session_id: str | None = None,
    strict_mode: bool = True,
    temperature_overrides: dict | None = None,
    history: list[dict] | None = None,  # NEW
) -> tuple[str, list[dict]]:
    # ... existing code ...

    # Pass history to optimist prompt
    draft_prompt = self._build_optimist_prompt(query, context_props, history=history)
```

---

**B3: Update Optimist Prompt to Include History** — 20 min

File: `services/rag_engine/pipelines/verified.py`

```python
def _build_optimist_prompt(
    self,
    query: str,
    context_docs: list[dict],
    history: list[dict] | None = None,  # NEW PARAMETER
) -> str:
    """
    Builds an enhanced prompt for the optimist (draft generation) role.

    Parameters
    ----------
    query : str
        The original query from the user.
    context_docs : list[dict]
        Retrieved documents with 'content' and 'source' keys.
    history : list[dict] | None
        Previous conversation turns with 'question' and 'answer' keys.
        Limited to last 5 turns to prevent context overflow.

    Returns
    -------
    str
        The formatted optimist prompt with sources and conversation context.
    """
    # ... existing context formatting ...

    # NEW: Format conversation history
    history_section = ""
    if history:
        # Limit to last 5 turns to prevent context overflow
        recent_history = history[-5:] if len(history) > 5 else history
        history_section = "\n\n## Previous Conversation\n"
        for turn in recent_history:
            q = turn.get('question', '')[:500]  # Truncate long questions
            a = turn.get('answer', '')[:1000]   # Truncate long answers
            history_section += f"User: {q}\nAssistant: {a}\n\n"

    if self.optimist_strictness == OPTIMIST_STRICTNESS_BALANCED:
        return f"""You are a helpful assistant...

SOURCES:
{context_str}
{history_section}
QUESTION: {query}

Write a helpful answer..."""

    # Default: STRICT mode
    return f"""You are a CAREFUL, GROUNDED assistant...

SOURCES:
{context_str}
{history_section}
QUESTION: {query}

Write a helpful answer that cites [Source N] for each claim:"""
```

---

**B4: Update Server Endpoint to Pass History** — 10 min

File: `services/rag_engine/server.py`

Update both `/rag/verified` and `/rag/verified/stream` endpoints:

```python
@app.post("/rag/verified", response_model=RAGEngineResponse)
async def run_verified_rag(request: VerifiedRAGRequest):
    # ... existing code ...

    # Convert history if provided
    history = None
    if request.history:
        history = [{"question": t.question, "answer": t.answer} for t in request.history]

    answer, source_docs = await pipeline.run(
        request.query,
        request.session_id,
        request.strict_mode,
        temperature_overrides=temp_overrides,
        history=history,  # NEW
    )


@app.post("/rag/verified/stream")
async def run_verified_rag_streaming(request: VerifiedRAGRequest):
    # ... inside run_pipeline() ...

    history = None
    if request.history:
        history = [{"question": t.question, "answer": t.answer} for t in request.history]

    answer, sources = await pipeline.run_with_progress(
        request.query,
        progress_callback=progress_callback,
        session_id=request.session_id,
        strict_mode=request.strict_mode,
        temperature_overrides=temp_overrides,
        history=history,  # NEW
    )
```

---

**B5: Update Go to Pass History** — 25 min

File: `services/orchestrator/handlers/chat_streaming.go`

Step 1: Update `HandleVerifiedRAGStream` to load history:

```go
func (h *streamingChatHandler) HandleVerifiedRAGStream(c *gin.Context) {
    // ... existing setup code ...

    // Step 5.5: Load session history for conversation context (NEW)
    var sessionHistory []ConversationTurn
    if sessionID != "" {
        var historyErr error
        sessionHistory, historyErr = h.loadSessionHistory(ctx, sessionID)
        if historyErr != nil {
            slog.Warn("failed to load session history, continuing without",
                "session_id", sessionID,
                "error", historyErr,
            )
        }
        span.SetAttributes(attribute.Int("session.history_turns", len(sessionHistory)))
    }

    // Step 6: Call Python's verified streaming endpoint (UPDATED)
    result, err := h.streamFromVerifiedPipeline(ctx, &req, sseWriter, verbosity, sessionID, sessionHistory, span)
    // ...
}
```

Step 2: Update `buildVerifiedStreamRequest` signature and implementation:

```go
func (h *streamingChatHandler) buildVerifiedStreamRequest(
    req *datatypes.ChatRAGRequest,
    sessionHistory []ConversationTurn,  // NEW PARAMETER
) map[string]interface{} {
    body := map[string]interface{}{
        "query":       req.Message,
        "session_id":  req.SessionId,
        "strict_mode": req.StrictMode,
    }

    // NEW: Add conversation history
    if len(sessionHistory) > 0 {
        historyTurns := make([]map[string]string, len(sessionHistory))
        for i, turn := range sessionHistory {
            historyTurns[i] = map[string]string{
                "question": turn.Question,
                "answer":   turn.Answer,
            }
        }
        body["history"] = historyTurns
    }

    // Existing temperature overrides logic...
    if req.TemperatureOverrides != nil {
        tempOverrides := req.TemperatureOverrides.ToMap()
        if tempOverrides != nil {
            body["temperature_overrides"] = tempOverrides
        }
    }

    return body
}
```

Step 3: Update `streamFromVerifiedPipeline` to accept history:

```go
func (h *streamingChatHandler) streamFromVerifiedPipeline(
    ctx context.Context,
    req *datatypes.ChatRAGRequest,
    writer SSEWriter,
    verbosity int,
    sessionID string,
    sessionHistory []ConversationTurn,  // NEW PARAMETER
    span trace.Span,
) (*verifiedStreamResult, error) {
    // ... existing code ...

    // Build request body (UPDATED)
    requestBody := h.buildVerifiedStreamRequest(req, sessionHistory)
    // ...
}
```

---

#### Phase C: Code Quality (P2-1)

**C1: Refactor Retrieval Deduplication** — 30 min

File: `services/rag_engine/pipelines/verified.py`

Replace duplicated retrieval logic in `run()` with parent delegation:

```python
async def run(
    self,
    query: str,
    session_id: str | None = None,
    strict_mode: bool = True,
    temperature_overrides: dict | None = None,
    history: list[dict] | None = None,
) -> tuple[str, list[dict]]:
    # ... temperature override setup ...

    with tracer.start_as_current_span("verified_pipeline.run") as root_span:
        # ... span attributes ...

        # A. Retrieve Data (REFACTORED - use parent)
        with tracer.start_as_current_span("verified_pipeline.retrieve") as retrieve_span:
            chunks, context_text, has_relevant = await super().retrieve_only(
                query, session_id, strict_mode, max_chunks=self.top_k_final
            )

            retrieve_span.set_attribute("retrieved.chunk_count", len(chunks))
            retrieve_span.set_attribute("retrieved.has_relevant", has_relevant)

            if not has_relevant and strict_mode:
                root_span.set_attribute("result", "no_relevant_docs")
                root_span.set_status(Status(StatusCode.OK))
                return NO_RELEVANT_DOCS_MESSAGE, []

        # Reconstruct context_docs format for downstream methods
        context_docs = [
            {
                "properties": {
                    "content": c["content"],
                    "source": c["source"]
                },
                "metadata": {"rerank_score": c.get("rerank_score")}
            }
            for c in chunks
        ]
        context_props = [d["properties"] for d in context_docs]

        # B. Continue with existing draft generation, verification loop, etc.
        # ...
```

Also update `run_with_progress()` similarly.

---

#### Phase D: Add Gemini Backend

**D1: Implement Gemini Support** — 1 hour

File: `services/rag_engine/pipelines/base.py`

```python
# In __init__, add Gemini config:
self.gemini_model = config.get("gemini_model", "gemini-1.5-flash")
self.gemini_api_key = self._read_secret("gemini_api_key")

# In _call_llm(), add Gemini backend:
elif self.llm_backend == "gemini":
    if not self.gemini_api_key:
        raise ValueError("Gemini API key secret not configured")

    effective_model = model_override if model_override else self.gemini_model
    if model_override:
        logger.debug(f"Using model override: {model_override} (default: {self.gemini_model})")

    api_url = f"https://generativelanguage.googleapis.com/v1beta/models/{effective_model}:generateContent"
    headers["x-goog-api-key"] = self.gemini_api_key

    payload = {
        "contents": [{"parts": [{"text": prompt}]}],
        "generationConfig": {
            "temperature": generation_params["temperature"],
            "maxOutputTokens": generation_params["max_tokens"],
            "topK": generation_params["top_k"],
            "topP": generation_params["top_p"],
            "stopSequences": generation_params["stop"] if generation_params["stop"] else []
        }
    }

# In response parsing, add Gemini:
elif self.llm_backend == "gemini":
    candidates = resp_data.get("candidates", [])
    if candidates and len(candidates) > 0:
        content = candidates[0].get("content", {})
        parts = content.get("parts", [])
        if parts and len(parts) > 0:
            answer = parts[0].get("text", "").strip()
```

File: `services/rag_engine/server.py`

```python
# Add Gemini to URL configuration:
GEMINI_MODEL = os.getenv("GEMINI_MODEL", "gemini-1.5-flash")

# In pipeline_config:
pipeline_config = {
    # ... existing ...
    "gemini_model": GEMINI_MODEL,
}
```

---

#### Phase E: Add Tests

**E1: Streaming Tests** — 45 min

File: `services/rag_engine/tests/test_verified_pipeline.py`

```python
@pytest.mark.asyncio
async def test_run_with_progress_emits_all_event_types(verified_pipeline, mock_llm):
    """Verify all progress event types are emitted during execution."""
    events_received = []

    async def capture_callback(event: ProgressEvent):
        events_received.append(event.event_type)

    mock_llm.return_value = '{"is_verified": true, "reasoning": "ok", "hallucinations": []}'

    await verified_pipeline.run_with_progress(
        query="What is X?",
        progress_callback=capture_callback
    )

    expected_types = [
        ProgressEventType.RETRIEVAL_START,
        ProgressEventType.RETRIEVAL_COMPLETE,
        ProgressEventType.DRAFT_START,
        ProgressEventType.DRAFT_COMPLETE,
        ProgressEventType.SKEPTIC_AUDIT_START,
        ProgressEventType.SKEPTIC_AUDIT_COMPLETE,
        ProgressEventType.VERIFICATION_COMPLETE,
    ]

    for expected in expected_types:
        assert expected in events_received, f"Missing event: {expected}"


@pytest.mark.asyncio
async def test_streaming_callback_error_doesnt_crash(verified_pipeline, mock_llm):
    """Verify pipeline continues if callback raises exception."""
    async def failing_callback(event: ProgressEvent):
        raise RuntimeError("Callback failed!")

    mock_llm.return_value = '{"is_verified": true, "reasoning": "ok", "hallucinations": []}'

    # Should not raise
    answer, sources = await verified_pipeline.run_with_progress(
        query="What is X?",
        progress_callback=failing_callback
    )

    assert answer is not None
```

---

**E2: History Tests** — 30 min

```python
@pytest.mark.asyncio
async def test_optimist_includes_conversation_history(verified_pipeline, mock_llm):
    """Verify conversation history is included in optimist prompt."""
    history = [
        {"question": "What is Chrysler?", "answer": "Chrysler is an automotive company."},
        {"question": "When was it founded?", "answer": "Chrysler was founded in 1925."},
    ]

    captured_prompt = None
    original_call_llm = verified_pipeline._call_llm

    async def capture_prompt(prompt, **kwargs):
        nonlocal captured_prompt
        captured_prompt = prompt
        return '{"is_verified": true, "reasoning": "ok", "hallucinations": []}'

    verified_pipeline._call_llm = capture_prompt

    await verified_pipeline.run(query="Tell me more", history=history)

    assert "Previous Conversation" in captured_prompt
    assert "What is Chrysler?" in captured_prompt
    assert "Chrysler is an automotive company" in captured_prompt


@pytest.mark.asyncio
async def test_history_limited_to_5_turns(verified_pipeline):
    """Verify only last 5 history turns are included."""
    history = [{"question": f"Q{i}", "answer": f"A{i}"} for i in range(10)]

    prompt = verified_pipeline._build_optimist_prompt(
        query="Current question",
        context_docs=[{"content": "test", "source": "test.txt"}],
        history=history
    )

    # Should include Q5-Q9 (last 5), not Q0-Q4
    assert "Q5" in prompt
    assert "Q9" in prompt
    assert "Q0" not in prompt
    assert "Q4" not in prompt
```

---

**E3: Multi-Provider Tests** — 30 min

```python
@pytest.mark.asyncio
async def test_temperature_override_applied_openai(mock_httpx):
    """Verify temperature parameter is used in OpenAI backend."""
    config = {
        "llm_backend_type": "openai",
        "llm_service_url": "https://api.openai.com/v1",
        # ... other config
    }
    pipeline = BaseRAGPipeline(mock_weaviate, config)
    pipeline.openai_api_key = "test-key"

    # Capture the request
    captured_payload = None
    async def capture_request(*args, **kwargs):
        nonlocal captured_payload
        captured_payload = kwargs.get('json', {})
        return MockResponse({"choices": [{"message": {"content": "test"}}]})

    mock_httpx.post = capture_request

    await pipeline._call_llm("test prompt", temperature=0.1)

    assert captured_payload["temperature"] == 0.1, "Temperature override not applied"
```

---

### Updated Implementation Priority Matrix

| Priority | ID | Item | Effort | Impact | Status |
|----------|-----|------|--------|--------|--------|
| **1** | A1 | Fix temperature bug (OpenAI/Local) | 15 min | Critical | 🔴 TODO |
| **2** | B1-B5 | Complete conversation history (P7) | 1.5 hr | High | 🔴 TODO |
| **3** | C1 | Refactor retrieval deduplication | 30 min | Medium | 🔴 TODO |
| **4** | D1 | Add Gemini backend | 1 hr | High | 🔴 TODO |
| **5** | E1-E3 | Add missing tests | 2 hr | Medium | 🔴 TODO |
| **6** | P10-1 | Add retry logic for LLM calls | 1 hr | Medium | 🔴 TODO |
| **7** | P4-5 | Source metadata in evidence | 30 min | Low | ⚪ Deferred |
| **8** | P4-6 | Confidence scoring | 2 hr | Medium | ⚪ Deferred |
| **9** | P5-8 | Context window management | 2 hr | Medium | ⚪ Deferred |
| **10** | P11-1 | Mixed-model configuration | 2 hr | Low | ⚪ Deferred |

**Total Remaining Effort:** ~7 hours for TODO items

---

### Acceptance Criteria Updates

#### P7 Conversation History (Updated)
- [x] Go `loadSessionHistory()` loads from Weaviate
- [ ] Go `buildVerifiedStreamRequest()` includes history
- [ ] Python `VerifiedRAGRequest` has history field
- [ ] Python `run()` and `run_with_progress()` accept history
- [ ] Python `_build_optimist_prompt()` includes history section
- [ ] History limited to last 5 turns
- [ ] Follow-up queries work: "Give me more details"

#### P8 Multi-Provider Support (New)
- [ ] Temperature overrides work with OpenAI backend
- [ ] Temperature overrides work with Local backend
- [ ] Gemini backend implemented and tested
- [ ] Documentation for provider configuration

#### P9 Test Coverage (New)
- [ ] Streaming event tests added
- [ ] History tests added
- [ ] Multi-provider tests added
- [ ] Test coverage > 80% for verified.py

---

## Appendix: Complete Fixed Code

See accompanying implementation in the next commit after design approval.
