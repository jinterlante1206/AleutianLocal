"""
// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.
"""
import asyncio
import logging
import json
import os
import time
from pathlib import Path

import weaviate
import yaml

from opentelemetry import trace
from opentelemetry.trace import Status, StatusCode

# Import our new datatypes
from datatypes.verified import (
    SkepticAuditResult,
    SkepticAuditRequest,
    RefinerRequest,
    VerificationState,
    # P6 Progress Streaming datatypes
    ProgressEvent,
    ProgressEventType,
    SkepticAuditDetails,
    RetrievalDetails,
)
from typing import Callable, Awaitable
from .reranking import RerankingPipeline
from .base import RERANK_SCORE_THRESHOLD, NO_RELEVANT_DOCS_MESSAGE

logger = logging.getLogger(__name__)

# OpenTelemetry tracer for verified pipeline operations
# This tracer is compatible with Jaeger, Arize Phoenix, and other OTEL collectors
tracer = trace.get_tracer(
    "aleutian.rag.verified",
    schema_url="https://opentelemetry.io/schemas/1.21.0"
)

# =============================================================================
# CONFIGURATION CONSTANTS
# =============================================================================
# All constants support environment variable overrides for 12-factor app compliance.
# Priority: Request-level > Config dict > Environment variable > Default

# --- Verification Loop ---
DEFAULT_MAX_VERIFICATION_ATTEMPTS = 3
MIN_VERIFICATION_ATTEMPTS = 1
MAX_VERIFICATION_ATTEMPTS_LIMIT = 5  # Safety cap to prevent runaway loops

# --- Refinement Quality ---
MIN_ANSWER_LENGTH = 10  # Minimum acceptable answer length after refinement
STALL_SIMILARITY_THRESHOLD = 0.95  # If answers are >95% similar, consider it stalled

# --- Weaviate ---
VERIFICATION_LOG_COLLECTION = "VerificationLog"

# --- Evidence Formatting ---
# Environment: VERIFIED_MAX_EVIDENCE_LENGTH
DEFAULT_MAX_EVIDENCE_LENGTH = 2000  # Max chars per document in evidence


def _get_env_float(name: str, default: float) -> float:
    """Get float from environment variable with fallback."""
    val = os.getenv(name)
    if val is None:
        return default
    try:
        return float(val)
    except ValueError:
        logger.warning(f"Invalid {name}='{val}', using default {default}")
        return default


def _get_env_int(name: str, default: int) -> int:
    """Get int from environment variable with fallback."""
    val = os.getenv(name)
    if val is None:
        return default
    try:
        return int(val)
    except ValueError:
        logger.warning(f"Invalid {name}='{val}', using default {default}")
        return default


def _get_env_str(name: str, default: str) -> str:
    """Get string from environment variable with fallback."""
    return os.getenv(name, default)


# --- Temperature Defaults (P4-2 Enhanced) ---
# Environment variables: VERIFIED_OPTIMIST_TEMPERATURE, VERIFIED_SKEPTIC_TEMPERATURE, VERIFIED_REFINER_TEMPERATURE
# Lower temperature = more deterministic/consistent
# Higher temperature = more creative/varied
# Note: Some local models (e.g., Ollama) may return empty responses at very low temperatures.
# Values below 0.5 can cause issues; 0.6-0.7 is a safe default for most models.
DEFAULT_OPTIMIST_TEMPERATURE = _get_env_float("VERIFIED_OPTIMIST_TEMPERATURE", 0.6)
DEFAULT_SKEPTIC_TEMPERATURE = _get_env_float("VERIFIED_SKEPTIC_TEMPERATURE", 0.6)
DEFAULT_REFINER_TEMPERATURE = _get_env_float("VERIFIED_REFINER_TEMPERATURE", 0.6)

# --- Evidence Length ---
MAX_EVIDENCE_CONTENT_LENGTH = _get_env_int("VERIFIED_MAX_EVIDENCE_LENGTH", DEFAULT_MAX_EVIDENCE_LENGTH)

# --- Optimist Strictness Mode (P4-3 Enhanced) ---
# Environment: VERIFIED_OPTIMIST_STRICTNESS
# Options: "strict" (default) - requires explicit source citations
#          "balanced" - prefers sources but allows synthesis
OPTIMIST_STRICTNESS_STRICT = "strict"
OPTIMIST_STRICTNESS_BALANCED = "balanced"
DEFAULT_OPTIMIST_STRICTNESS = _get_env_str("VERIFIED_OPTIMIST_STRICTNESS", OPTIMIST_STRICTNESS_STRICT)

# --- Few-Shot Examples Path (P4-4 Enhanced) ---
# Environment: VERIFIED_SKEPTIC_EXAMPLES_PATH
# If set, loads custom few-shot examples from this YAML file
DEFAULT_SKEPTIC_EXAMPLES_PATH = _get_env_str("VERIFIED_SKEPTIC_EXAMPLES_PATH", "")

# --- History Answer Truncation (P7) ---
# Environment: VERIFIED_HISTORY_ANSWER_MAX_LENGTH
# Maximum characters for previous answers in conversation history.
# Longer answers are truncated with "..." to prevent context overflow.
DEFAULT_HISTORY_ANSWER_MAX_LENGTH = _get_env_int("VERIFIED_HISTORY_ANSWER_MAX_LENGTH", 500)

# =============================================================================
# HARDCODED FEW-SHOT EXAMPLES (Fallback if no file configured)
# =============================================================================
DEFAULT_SKEPTIC_EXAMPLES = [
    {
        "type": "verified",
        "query": "What is the capital of France?",
        "answer": "Paris is the capital of France [Source 0].",
        "evidence": "[Source 0]: France is a country in Western Europe. Its capital city is Paris.",
        "output": {
            "is_verified": True,
            "reasoning": "The claim that Paris is the capital of France is directly stated in Source 0.",
            "hallucinations": [],
            "missing_evidence": []
        }
    },
    {
        "type": "hallucination",
        "query": "Tell me about Python programming.",
        "answer": "Python was created by Guido van Rossum in 1991. It is the most popular programming language in the world and is used by over 10 million developers.",
        "evidence": "[Source 0]: Python is a programming language created by Guido van Rossum. It was first released in 1991.",
        "output": {
            "is_verified": False,
            "reasoning": "While Source 0 confirms the creator and release year, two claims lack evidence: (1) 'most popular programming language' and (2) '10 million developers'. These statistics are not in the sources.",
            "hallucinations": [
                "Python is the most popular programming language in the world",
                "Python is used by over 10 million developers"
            ],
            "missing_evidence": [
                "Statistics on Python's popularity ranking",
                "Developer usage numbers"
            ]
        }
    }
]


class VerifiedRAGPipeline(RerankingPipeline):
    """
    Orchestrates the Self-Correcting RAG Loop using strictly defined datatypes.

    Configuration Hierarchy (highest priority first):
    1. Request-level overrides (passed to run() method)
    2. Config dict (passed to __init__)
    3. Environment variables (VERIFIED_*)
    4. Hardcoded defaults

    Environment Variables
    ---------------------
    VERIFIED_OPTIMIST_TEMPERATURE : float
        Temperature for draft generation (default: 0.6)
    VERIFIED_SKEPTIC_TEMPERATURE : float
        Temperature for skeptic audits (default: 0.2)
    VERIFIED_REFINER_TEMPERATURE : float
        Temperature for refinement (default: 0.4)
    VERIFIED_MAX_EVIDENCE_LENGTH : int
        Max chars per document in evidence (default: 2000)
    VERIFIED_OPTIMIST_STRICTNESS : str
        "strict" (default) or "balanced"
    VERIFIED_SKEPTIC_EXAMPLES_PATH : str
        Path to YAML file with custom few-shot examples
    """

    def __init__(self, weaviate_client: weaviate.WeaviateClient, config: dict):
        super().__init__(weaviate_client, config)
        self.skeptic_model = config.get("skeptic_model")

        # Configurable max verification attempts with bounds validation
        configured_attempts = config.get("max_verification_attempts", DEFAULT_MAX_VERIFICATION_ATTEMPTS)
        try:
            configured_attempts = int(configured_attempts)
        except (TypeError, ValueError):
            logger.warning(f"Invalid max_verification_attempts '{configured_attempts}', using default")
            configured_attempts = DEFAULT_MAX_VERIFICATION_ATTEMPTS

        # Enforce bounds
        if configured_attempts < MIN_VERIFICATION_ATTEMPTS:
            logger.warning(f"max_verification_attempts {configured_attempts} below minimum, using {MIN_VERIFICATION_ATTEMPTS}")
            configured_attempts = MIN_VERIFICATION_ATTEMPTS
        elif configured_attempts > MAX_VERIFICATION_ATTEMPTS_LIMIT:
            logger.warning(f"max_verification_attempts {configured_attempts} exceeds limit, capping at {MAX_VERIFICATION_ATTEMPTS_LIMIT}")
            configured_attempts = MAX_VERIFICATION_ATTEMPTS_LIMIT

        self.max_verification_attempts = configured_attempts

        # Role-specific temperatures (P4-2)
        # Priority: config dict > env var > default
        self.optimist_temperature = self._parse_temperature(
            config.get("optimist_temperature", DEFAULT_OPTIMIST_TEMPERATURE),
            "optimist", DEFAULT_OPTIMIST_TEMPERATURE
        )
        self.skeptic_temperature = self._parse_temperature(
            config.get("skeptic_temperature", DEFAULT_SKEPTIC_TEMPERATURE),
            "skeptic", DEFAULT_SKEPTIC_TEMPERATURE
        )
        self.refiner_temperature = self._parse_temperature(
            config.get("refiner_temperature", DEFAULT_REFINER_TEMPERATURE),
            "refiner", DEFAULT_REFINER_TEMPERATURE
        )

        # Evidence max length (configurable)
        self.max_evidence_length = config.get("max_evidence_length", MAX_EVIDENCE_CONTENT_LENGTH)

        # Optimist strictness mode (P4-3 Enhanced)
        self.optimist_strictness = config.get("optimist_strictness", DEFAULT_OPTIMIST_STRICTNESS)
        if self.optimist_strictness not in (OPTIMIST_STRICTNESS_STRICT, OPTIMIST_STRICTNESS_BALANCED):
            logger.warning(f"Invalid optimist_strictness '{self.optimist_strictness}', using '{OPTIMIST_STRICTNESS_STRICT}'")
            self.optimist_strictness = OPTIMIST_STRICTNESS_STRICT

        # Few-shot examples (P4-4 Enhanced)
        examples_path = config.get("skeptic_examples_path", DEFAULT_SKEPTIC_EXAMPLES_PATH)
        self.skeptic_examples = self._load_skeptic_examples(examples_path)

        logger.info(
            f"VerifiedRAGPipeline initialized. "
            f"Skeptic Model: {self.skeptic_model}, "
            f"Max Attempts: {self.max_verification_attempts}, "
            f"Temps: optimist={self.optimist_temperature}, skeptic={self.skeptic_temperature}, refiner={self.refiner_temperature}, "
            f"Strictness: {self.optimist_strictness}, "
            f"Examples: {len(self.skeptic_examples)} loaded"
        )

    def _load_skeptic_examples(self, path: str) -> list[dict]:
        """
        Load few-shot examples from YAML file or use defaults.

        Parameters
        ----------
        path : str
            Path to YAML file containing examples. Empty string uses defaults.

        Returns
        -------
        list[dict]
            List of example dictionaries with keys: type, query, answer, evidence, output

        Notes
        -----
        YAML file format:
        ```yaml
        examples:
          - type: verified
            query: "..."
            answer: "..."
            evidence: "..."
            output:
              is_verified: true
              reasoning: "..."
              hallucinations: []
              missing_evidence: []
        ```
        """
        if not path:
            logger.debug("No skeptic examples path configured, using defaults")
            return DEFAULT_SKEPTIC_EXAMPLES

        try:
            examples_path = Path(path)
            if not examples_path.exists():
                logger.warning(f"Skeptic examples file not found: {path}, using defaults")
                return DEFAULT_SKEPTIC_EXAMPLES

            with open(examples_path, 'r', encoding='utf-8') as f:
                data = yaml.safe_load(f)

            if not data or "examples" not in data:
                logger.warning(f"Invalid skeptic examples file format: {path}, using defaults")
                return DEFAULT_SKEPTIC_EXAMPLES

            examples = data["examples"]
            if not isinstance(examples, list) or len(examples) == 0:
                logger.warning(f"No examples found in {path}, using defaults")
                return DEFAULT_SKEPTIC_EXAMPLES

            # Validate each example has required keys
            required_keys = {"type", "query", "answer", "evidence", "output"}
            valid_examples = []
            for i, ex in enumerate(examples):
                if not isinstance(ex, dict):
                    logger.warning(f"Example {i} is not a dict, skipping")
                    continue
                missing = required_keys - set(ex.keys())
                if missing:
                    logger.warning(f"Example {i} missing keys {missing}, skipping")
                    continue
                valid_examples.append(ex)

            if not valid_examples:
                logger.warning(f"No valid examples in {path}, using defaults")
                return DEFAULT_SKEPTIC_EXAMPLES

            logger.info(f"Loaded {len(valid_examples)} skeptic examples from {path}")
            return valid_examples

        except yaml.YAMLError as e:
            logger.error(f"Failed to parse YAML from {path}: {e}, using defaults")
            return DEFAULT_SKEPTIC_EXAMPLES
        except Exception as e:
            logger.error(f"Failed to load skeptic examples from {path}: {e}, using defaults")
            return DEFAULT_SKEPTIC_EXAMPLES

    # --- 1. Helper: Prompt Builders (Single Responsibility: Formatting) ---

    def _parse_temperature(self, value, role_name: str, default: float) -> float:
        """
        Parse and validate temperature value for a role.

        Parameters
        ----------
        value : Any
            The temperature value from config (could be str, float, int, None).
        role_name : str
            The role name for logging purposes.
        default : float
            Default temperature if parsing fails.

        Returns
        -------
        float
            Validated temperature between 0.0 and 2.0.
        """
        try:
            temp = float(value) if value is not None else default
        except (TypeError, ValueError):
            logger.warning(f"Invalid {role_name}_temperature '{value}', using default {default}")
            return default

        # Clamp to valid range
        if temp < 0.0:
            logger.warning(f"{role_name}_temperature {temp} below 0, clamping to 0.0")
            temp = 0.0
        elif temp > 2.0:
            logger.warning(f"{role_name}_temperature {temp} above 2.0, clamping to 2.0")
            temp = 2.0

        return temp

    def _format_evidence(self, context_docs: list[dict]) -> str:
        """
        Formats list of docs into a labeled string for the LLM.

        Preserves content formatting (newlines, indentation) to maintain
        readability of code snippets and structured text. Truncates long
        content to prevent token overflow.

        Parameters
        ----------
        context_docs : list[dict]
            List of document dictionaries with 'content' key.

        Returns
        -------
        str
            Formatted evidence string with source labels.

        Notes
        -----
        Max length per document is configurable via:
        - Config: max_evidence_length
        - Env: VERIFIED_MAX_EVIDENCE_LENGTH
        """
        evidence_parts = []
        max_len = self.max_evidence_length
        for i, doc in enumerate(context_docs):
            content = doc.get('content', '')

            # Truncate long content but preserve formatting
            if len(content) > max_len:
                content = content[:max_len] + "\n... [truncated]"

            # Preserve formatting - don't replace newlines
            evidence_parts.append(f"[Source {i}]:\n{content}")

        return "\n\n---\n\n".join(evidence_parts)

    def _get_rerank_score(self, doc: dict) -> float | None:
        """
        Safely extract rerank score from document metadata.

        This helper provides consistent access to rerank_score regardless of
        whether metadata is a dict, object, or missing entirely. It also
        handles type coercion from string to float.

        Parameters
        ----------
        doc : dict
            A document dictionary with optional 'metadata' key.

        Returns
        -------
        float | None
            The rerank score if available and valid (0.0-1.0), None otherwise.
        """
        metadata = doc.get("metadata")
        if metadata is None:
            return None

        # Extract value using appropriate access pattern
        value = None
        if hasattr(metadata, "rerank_score"):
            value = metadata.rerank_score
        elif isinstance(metadata, dict):
            value = metadata.get("rerank_score")

        # Type coercion and validation
        return self._coerce_to_float(value, min_val=0.0, max_val=1.0)

    def _get_distance(self, doc: dict) -> float | None:
        """
        Safely extract distance from document metadata.

        This helper provides consistent access to distance regardless of
        whether metadata is a dict, object, or missing entirely. It also
        handles type coercion from string to float.

        Parameters
        ----------
        doc : dict
            A document dictionary with optional 'metadata' key.

        Returns
        -------
        float | None
            The distance if available and valid (>= 0), None otherwise.
        """
        metadata = doc.get("metadata")
        if metadata is None:
            return None

        # Extract value using appropriate access pattern
        value = None
        if hasattr(metadata, "distance"):
            value = metadata.distance
        elif isinstance(metadata, dict):
            value = metadata.get("distance")

        # Type coercion and validation (distance should be non-negative)
        return self._coerce_to_float(value, min_val=0.0)

    def _coerce_to_float(
        self,
        value,
        min_val: float | None = None,
        max_val: float | None = None
    ) -> float | None:
        """
        Coerce a value to float with optional bounds checking.

        Handles string-to-float conversion and validates bounds.

        Parameters
        ----------
        value : Any
            The value to coerce (can be float, int, str, or None).
        min_val : float | None
            Optional minimum valid value.
        max_val : float | None
            Optional maximum valid value.

        Returns
        -------
        float | None
            The coerced float if valid, None otherwise.
        """
        if value is None:
            return None

        try:
            # Handle string values (e.g., "0.85")
            if isinstance(value, str):
                value = value.strip()
                if not value:
                    return None
                result = float(value)
            elif isinstance(value, (int, float)):
                result = float(value)
            else:
                return None

            # Bounds checking
            if min_val is not None and result < min_val:
                logger.debug(f"Value {result} below min {min_val}, returning None")
                return None
            if max_val is not None and result > max_val:
                logger.debug(f"Value {result} above max {max_val}, returning None")
                return None

            return result

        except (ValueError, TypeError):
            return None

    def _has_valid_score(self, doc: dict, threshold: float = 0.0) -> bool:
        """
        Check if a document has a valid rerank score above threshold.

        Convenience method for filtering documents by score.

        Parameters
        ----------
        doc : dict
            A document dictionary with optional 'metadata' key.
        threshold : float
            Minimum score to consider valid. Default: 0.0.

        Returns
        -------
        bool
            True if document has a valid score >= threshold.
        """
        score = self._get_rerank_score(doc)
        return score is not None and score >= threshold

    def _is_refinement_stalled(self, old_answer: str, new_answer: str) -> bool:
        """
        Detect if refinement is stalled (answer didn't change meaningfully).

        This prevents infinite loops where the refiner keeps producing
        essentially the same answer that fails verification.

        Parameters
        ----------
        old_answer : str
            The answer before refinement.
        new_answer : str
            The answer after refinement.

        Returns
        -------
        bool
            True if the answers are too similar (stalled), False otherwise.
        """
        if not old_answer or not new_answer:
            return False

        # Normalize for comparison
        old_normalized = old_answer.strip().lower()
        new_normalized = new_answer.strip().lower()

        # Exact match is definitely stalled
        if old_normalized == new_normalized:
            return True

        # Check character-level similarity using simple ratio
        # This catches cases where only minor punctuation changed
        if len(old_normalized) == 0 or len(new_normalized) == 0:
            return False

        # Simple similarity: ratio of matching characters
        shorter = min(len(old_normalized), len(new_normalized))
        longer = max(len(old_normalized), len(new_normalized))

        # If lengths are very different, not stalled
        if shorter / longer < 0.8:
            return False

        # Count matching prefix/suffix
        matching = 0
        for i in range(shorter):
            if old_normalized[i] == new_normalized[i]:
                matching += 1
            else:
                break

        similarity = matching / longer
        if similarity >= STALL_SIMILARITY_THRESHOLD:
            logger.debug(f"Refinement stall detected: {similarity:.2%} similarity")
            return True

        return False

    def _validate_refined_answer(self, answer: str, original: str) -> tuple[bool, str]:
        """
        Validate that a refined answer meets quality requirements.

        Parameters
        ----------
        answer : str
            The refined answer to validate.
        original : str
            The original answer before refinement.

        Returns
        -------
        tuple[bool, str]
            (is_valid, reason) - True if valid, False with reason if not.
        """
        if not answer or not answer.strip():
            return False, "empty_answer"

        if len(answer.strip()) < MIN_ANSWER_LENGTH:
            return False, f"too_short_{len(answer.strip())}_chars"

        if self._is_refinement_stalled(original, answer):
            return False, "stalled_no_change"

        return True, "valid"

    def _build_optimist_prompt(
        self,
        query: str,
        context_docs: list[dict],
        relevant_history: list[dict] | None = None
    ) -> str:
        """
        Builds an enhanced prompt for the optimist (draft generation) role.

        This override of the base _build_prompt adds source-grounding instructions
        that encourage the LLM to cite sources and be explicit about which
        evidence supports each claim. This makes the skeptic's job easier
        and reduces initial hallucinations.

        Parameters
        ----------
        query : str
            The original query from the user.
        context_docs : list[dict]
            A list of document dictionaries with 'content' and 'source' keys.
        relevant_history : list[dict] | None
            Optional conversation history for follow-up context. Each dict:
            - "question": str - previous user query
            - "answer": str - previous AI response
            - "turn_number": int | None - turn number

        Returns
        -------
        str
            The formatted optimist prompt with source-grounding instructions.

        Notes
        -----
        P4-3 Enhancement: Adds explicit instructions to:
        - Cite source numbers for each fact
        - Be conservative and only claim what's directly stated
        - Flag uncertainty when evidence is partial

        P7 Enhancement: Adds conversation history section to provide context
        for follow-up queries like "tell me more". History is clearly labeled
        as "Conversation History (Memory)" to distinguish from "Knowledge Base (Facts)".

        Strictness modes (configurable via VERIFIED_OPTIMIST_STRICTNESS):
        - "strict": Every claim MUST have explicit source citation
        - "balanced": Prefers sources but allows minor synthesis
        """
        if not context_docs:
            context_str = "No relevant context found."
        else:
            # Format with numbered sources for easier citation
            context_parts = []
            for i, doc in enumerate(context_docs):
                source = doc.get('source', 'Unknown')
                content = doc.get('content', '')
                context_parts.append(f"[Source {i}] ({source}):\n{content}")
            context_str = "\n\n---\n\n".join(context_parts)

        # Format conversation history if provided (P7)
        history_str = ""
        if relevant_history:
            history_parts = []
            for turn in relevant_history:
                # Use 'or' to handle both missing key AND explicit None value
                turn_num = turn.get('turn_number') or '?'
                question = turn.get('question', '')
                answer = turn.get('answer', '')
                # Truncate long answers to avoid overwhelming context
                max_len = DEFAULT_HISTORY_ANSWER_MAX_LENGTH
                if len(answer) > max_len:
                    answer = answer[:max_len] + "..."
                history_parts.append(f"[Turn {turn_num}] User: {question}\n[Turn {turn_num}] AI: {answer}")
            history_str = "\n\n".join(history_parts)

        # Select prompt based on strictness mode
        if self.optimist_strictness == OPTIMIST_STRICTNESS_BALANCED:
            history_section = ""
            if history_str:
                history_section = f"""
# CONVERSATION HISTORY (Memory)
Use this to understand context and resolve pronouns (e.g., "it", "more", "that").
Do NOT cite conversation history as a source - only cite [Source N] from the Knowledge Base.

{history_str}

"""
            return f"""You are a helpful assistant. Answer the user's question based primarily on the provided sources.

# INSTRUCTIONS
1. Base your answer on the provided sources and cite them as [Source N] where applicable.
2. You may synthesize information across sources to provide a coherent answer.
3. If sources provide conflicting information, note the discrepancy.
4. If the sources don't fully address the question, you may provide context but clearly distinguish it from sourced facts.
5. Prefer explicit source citations when possible.
6. Use conversation history to understand what the user is referring to, but cite facts from sources.

# UPLOADED KNOWLEDGE BASE (Facts)
Use these sources for factual claims. Cite as [Source N].

{context_str}
{history_section}
# CURRENT QUESTION
{query}

Write a helpful answer that references sources where applicable:"""

        # Default: STRICT mode
        history_section = ""
        if history_str:
            history_section = f"""
# CONVERSATION HISTORY (Memory)
Use this ONLY to understand what the user is referring to (e.g., "tell me more" refers to previous topic).
Do NOT cite conversation history as a source - ONLY cite [Source N] from the Knowledge Base.
Do NOT repeat information already provided in conversation history.

{history_str}

"""
        return f"""You are a CAREFUL, GROUNDED assistant. Answer the user's question using ONLY the provided sources.

# CRITICAL INSTRUCTIONS
1. Every fact you state MUST be directly supported by a source - cite it as [Source N].
2. If a source doesn't explicitly state something, DON'T infer or assume it.
3. If sources provide conflicting information, note the discrepancy.
4. If the sources don't contain enough information to fully answer, say so clearly.
5. DO NOT use any prior knowledge - ONLY the sources below.
6. Use conversation history to understand context, but cite facts from sources.

# UPLOADED KNOWLEDGE BASE (Facts)
Use these sources for factual claims. Cite as [Source N].

{context_str}
{history_section}
# CURRENT QUESTION
{query}

Write a helpful answer that cites [Source N] for each claim:"""

    def _build_skeptic_prompt(self, req: SkepticAuditRequest) -> str:
        """
        Builds the skeptic audit prompt with few-shot examples.

        Parameters
        ----------
        req : SkepticAuditRequest
            Contains query, proposed_answer, and evidence_text.

        Returns
        -------
        str
            The formatted skeptic prompt with examples.

        Notes
        -----
        P4-4 Enhancement: Adds few-shot examples demonstrating:
        - A verified case (all claims supported)
        - A hallucination case (unsupported claims detected)

        Examples are loaded from:
        - Config: skeptic_examples_path
        - Env: VERIFIED_SKEPTIC_EXAMPLES_PATH
        - Fallback: DEFAULT_SKEPTIC_EXAMPLES (hardcoded)
        """
        # Build examples section from loaded examples
        examples_section = self._format_skeptic_examples()

        return f"""You are a SKEPTICAL FACT-CHECKER auditing someone else's answer for hallucinations.

CRITICAL RULES:
1. ASSUME THE ANSWER IS WRONG until proven right by evidence.
2. Each claim needs DIRECT, EXPLICIT support - no assumptions or inferences.
3. If a claim requires connecting multiple sources or "reading between the lines", mark it as unsupported.
4. Vague or partial matches = HALLUCINATION.

{examples_section}

=== NOW AUDIT THIS ===

USER QUERY: {req.query}

ANSWER TO AUDIT (treat this as potentially flawed):
{req.proposed_answer}

VERIFIED EVIDENCE (the ONLY truth source):
{req.evidence_text}

AUDIT PROCESS:
Step 1: Break the answer into individual factual claims.
Step 2: For EACH claim, find its EXACT match in evidence (quote source number).
Step 3: If no exact match exists, add to hallucinations list.
Step 4: List what evidence is missing to fully answer the query.

Output ONLY valid JSON (no markdown, no explanation before/after):
{{"is_verified": boolean, "reasoning": "string", "hallucinations": ["claim 1...", "claim 2..."], "missing_evidence": ["info 1...", "info 2..."]}}

REMEMBER: Being strict protects users from misinformation. When in doubt, mark as hallucination."""

    def _format_skeptic_examples(self) -> str:
        """
        Format loaded skeptic examples into prompt-ready text.

        Returns
        -------
        str
            Formatted examples section for the skeptic prompt.
        """
        if not self.skeptic_examples:
            return ""

        parts = []
        for i, ex in enumerate(self.skeptic_examples, 1):
            ex_type = ex.get("type", "example").upper()
            label = "VERIFIED" if ex_type == "VERIFIED" else "HALLUCINATION DETECTED"

            # Format the output JSON
            output = ex.get("output", {})
            output_json = json.dumps(output, ensure_ascii=False)

            parts.append(f"""=== EXAMPLE {i}: {label} ===
Query: "{ex.get('query', '')}"
Answer: "{ex.get('answer', '')}"
Evidence: "{ex.get('evidence', '')}"

Output:
{output_json}""")

        return "\n\n".join(parts)

    def _build_refiner_prompt(self, req: RefinerRequest) -> str:
        """
        Builds prompt for the Refiner to rewrite the answer removing hallucinations.

        Unlike the skeptic prompt which outputs JSON, this prompt asks for a
        refined prose answer that removes unsupported claims while keeping
        verified facts.

        Parameters
        ----------
        req : RefinerRequest
            Contains query, draft_answer, and audit_result with hallucinations.

        Returns
        -------
        str
            The formatted prompt for the refiner LLM.

        Notes
        -----
        This method includes defensive checks for malformed inputs:
        - None audit_result defaults to generic refinement request
        - Empty/None hallucinations list handled gracefully
        - Long draft answers are truncated to prevent token overflow
        """
        # Defensive: Handle None or missing audit_result
        if req.audit_result is None:
            logger.warning("RefinerRequest has None audit_result, using fallback prompt")
            return f"""Rewrite this answer to be more accurate and grounded:

QUERY: {req.query}

DRAFT: {(req.draft_answer or "")[:4000]}

Write an improved answer:"""

        # Format hallucinations as a bulleted list, filtering out None/empty values
        hallucinations = req.audit_result.hallucinations or []
        valid_hallucinations = [str(h) for h in hallucinations if h]

        if valid_hallucinations:
            hallucination_list = "\n".join(f"  - {h}" for h in valid_hallucinations)
        else:
            hallucination_list = "  (No specific hallucinations identified)"

        # Get reasoning with fallback
        reasoning = req.audit_result.reasoning or "(No reasoning provided)"

        # Truncate very long draft answers to prevent token overflow (keep ~4000 chars)
        draft_answer = req.draft_answer or ""
        if len(draft_answer) > 4000:
            draft_answer = draft_answer[:4000] + "\n... [truncated]"
            logger.warning(f"Truncated draft_answer from {len(req.draft_answer)} to 4000 chars")

        # Include evidence if available (P4-1 enhancement)
        evidence_section = ""
        if req.evidence_text:
            evidence_section = f"""
AVAILABLE EVIDENCE (use this to verify what you keep):
{req.evidence_text}

"""

        return f"""You are a CAREFUL ANSWER REFINER. Your task is to rewrite an answer
to remove hallucinations while preserving all verified facts.

ORIGINAL QUERY: {req.query}
{evidence_section}
DRAFT ANSWER (contains unsupported claims):
{draft_answer}

HALLUCINATIONS TO REMOVE:
{hallucination_list}

SKEPTIC'S ANALYSIS:
{reasoning}

REFINEMENT INSTRUCTIONS:
1. Keep all claims that ARE supported by evidence (not listed as hallucinations)
2. Remove or qualify claims marked as hallucinations
3. If removing hallucinations leaves the answer empty or meaningless, write:
   "Based on the available documents, I cannot fully answer this question."
4. Do NOT add new information - only keep what's verifiable in the evidence
5. Do NOT add disclaimers or meta-commentary about the refinement process
6. Maintain a helpful, natural tone

Write ONLY the refined answer below (no JSON, no explanation, no preamble):
"""

    # --- 2. Core Actions (Single Responsibility: Execution) ---

    def _extract_json(self, text: str) -> tuple[dict | None, str]:
        """
        Robustly extract JSON from LLM response with multiple fallback strategies.

        This method handles common LLM output quirks:
        - JSON wrapped in markdown code blocks
        - Preamble/postamble text around JSON
        - Nested JSON structures
        - Minor formatting issues
        - Control characters that break parsing

        Parameters
        ----------
        text : str
            The raw LLM response text that may contain JSON.

        Returns
        -------
        tuple[dict | None, str]
            A tuple containing:
            - The extracted JSON as a dict, or None if extraction failed
            - The strategy name that succeeded (for observability)

        Strategies Applied
        ------------------
        1. Direct parse: Try parsing the entire text as JSON
        2. Markdown extraction: Extract from ```json ... ``` blocks
        3. Balanced brace extraction: Find outermost balanced { } pair
        4. Repair common issues: Fix trailing commas, control chars, etc.
        """
        import re

        if not text or not text.strip():
            return None, "empty_input"

        # Pre-process: Remove control characters that break JSON parsing
        # Keep newlines and tabs as they're valid in JSON strings
        text = ''.join(char if char >= ' ' or char in '\n\t\r' else ' ' for char in text)
        text = text.strip()

        # Strategy 1: Direct parse (fastest, works for clean responses)
        try:
            result = json.loads(text)
            if isinstance(result, dict):
                return result, "direct_parse"
        except json.JSONDecodeError:
            pass

        # Strategy 2: Extract from markdown code block
        # Handles: ```json\n{...}\n``` or ```\n{...}\n```
        code_block_patterns = [
            (r'```json\s*([\s\S]*?)\s*```', "markdown_json"),
            (r'```\s*([\s\S]*?)\s*```', "markdown_generic"),
        ]
        for pattern, strategy_name in code_block_patterns:
            match = re.search(pattern, text, re.IGNORECASE)
            if match:
                try:
                    result = json.loads(match.group(1).strip())
                    if isinstance(result, dict):
                        return result, strategy_name
                except json.JSONDecodeError:
                    continue

        # Strategy 3: Balanced brace extraction (handles nested JSON)
        # This correctly handles {"key": {"nested": "value"}}
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
                        candidate = text[start:i + 1]
                        result = json.loads(candidate)
                        if isinstance(result, dict):
                            return result, "balanced_braces"
                    except json.JSONDecodeError:
                        # Reset and continue looking for another valid JSON
                        start = None
                        depth = 0

        # Strategy 4: Try to repair common JSON issues
        if "{" in text and "}" in text:
            first_brace = text.find("{")
            last_brace = text.rfind("}")
            if first_brace < last_brace:
                candidate = text[first_brace:last_brace + 1]

                # Fix 1: Trailing commas before } or ]
                candidate = re.sub(r',\s*([}\]])', r'\1', candidate)

                # Fix 2: Single quotes to double quotes (only if no double quotes)
                if '"' not in candidate and "'" in candidate:
                    candidate = candidate.replace("'", '"')

                # Fix 3: Unquoted keys (common LLM mistake)
                # {is_verified: true} -> {"is_verified": true}
                candidate = re.sub(r'{\s*(\w+)\s*:', r'{"\1":', candidate)
                candidate = re.sub(r',\s*(\w+)\s*:', r', "\1":', candidate)

                # Fix 4: Python-style booleans/None
                candidate = re.sub(r'\bTrue\b', 'true', candidate)
                candidate = re.sub(r'\bFalse\b', 'false', candidate)
                candidate = re.sub(r'\bNone\b', 'null', candidate)

                try:
                    result = json.loads(candidate)
                    if isinstance(result, dict):
                        return result, "repaired"
                except json.JSONDecodeError:
                    pass

        # Strategy 5: Last resort - try to find any JSON-like structure
        # Sometimes LLMs add text before/after like "Here is the JSON: {...}"
        json_pattern = r'\{[^{}]*(?:\{[^{}]*\}[^{}]*)*\}'
        matches = re.findall(json_pattern, text)
        for match in matches:
            try:
                result = json.loads(match)
                if isinstance(result, dict) and "is_verified" in result:
                    return result, "regex_fallback"
            except json.JSONDecodeError:
                continue

        # All strategies failed
        logger.warning(f"JSON extraction failed for text (first 200 chars): {text[:200]}")
        return None, "failed"

    async def _execute_skeptic_scan(
        self,
        req: SkepticAuditRequest,
        attempt: int = 1,
        temperature: float | None = None
    ) -> SkepticAuditResult:
        """
        Calls LLM to audit the answer and returns a typed result.

        This method is instrumented with OpenTelemetry for observability.
        Spans include LLM-specific attributes compatible with Arize Phoenix.

        Parameters
        ----------
        req : SkepticAuditRequest
            The audit request containing query, proposed answer, and evidence.
        attempt : int
            The current verification attempt number (1-indexed).
        temperature : float | None
            Optional temperature override for this call. If None, uses
            self.skeptic_temperature.

        Returns
        -------
        SkepticAuditResult
            The structured verdict from the skeptic.
        """
        # Use provided temperature or fall back to instance default
        effective_temp = temperature if temperature is not None else self.skeptic_temperature

        with tracer.start_as_current_span("verified_pipeline.skeptic_audit") as span:
            # Set span attributes for observability
            span.set_attribute("verification.attempt", attempt)
            span.set_attribute("query.length", len(req.query))
            span.set_attribute("proposed_answer.length", len(req.proposed_answer))
            span.set_attribute("evidence.length", len(req.evidence_text))

            prompt = self._build_skeptic_prompt(req)

            # LLM-specific attributes (Phoenix-compatible)
            span.set_attribute("llm.system", "skeptic")
            span.set_attribute("llm.provider", self.llm_backend)
            span.set_attribute("llm.model", self.skeptic_model or self.ollama_model)
            span.set_attribute("llm.temperature", effective_temp)
            span.set_attribute("llm.prompt.length", len(prompt))
            # Truncate prompt for span (full prompt can overwhelm trace storage)
            span.set_attribute("llm.prompt.preview", prompt[:500] + "..." if len(prompt) > 500 else prompt)

            # Call the LLM with skeptic-specific temperature
            response_str = await self._call_llm(
                prompt,
                model_override=self.skeptic_model,
                temperature=effective_temp
            )

            # Record completion attributes
            span.set_attribute("llm.completion.length", len(response_str) if response_str else 0)
            span.set_attribute("llm.completion.preview",
                (response_str[:500] + "...") if response_str and len(response_str) > 500 else (response_str or ""))

            # Defensive: Check for empty/None response
            if not response_str or not response_str.strip():
                logger.error("Skeptic returned empty response. Failing safe to is_verified=False.")
                span.set_status(Status(StatusCode.ERROR, "Empty LLM response"))
                span.set_attribute("skeptic.result", "error_empty_response")
                return SkepticAuditResult(
                    is_verified=False,
                    reasoning="Verification failed: skeptic returned empty response",
                    hallucinations=["Unable to verify - no skeptic response received"],
                    missing_evidence=["Re-run verification"]
                )

            try:
                # Use robust JSON extraction with multiple fallback strategies
                data, extraction_strategy = self._extract_json(response_str)

                # Record extraction strategy for observability (helps debug parsing issues)
                span.set_attribute("json.extraction_strategy", extraction_strategy)

                if data is None:
                    raise ValueError(f"No valid JSON object found in response (strategy: {extraction_strategy})")

                # Validate required fields exist before Pydantic
                if "is_verified" not in data:
                    raise ValueError("Missing required field 'is_verified'")

                # Ensure is_verified is a boolean (LLMs sometimes return strings)
                if isinstance(data.get("is_verified"), str):
                    data["is_verified"] = data["is_verified"].lower() in ("true", "yes", "1")

                # Ensure hallucinations is a list
                if "hallucinations" not in data or data["hallucinations"] is None:
                    data["hallucinations"] = []
                elif isinstance(data["hallucinations"], str):
                    # Handle case where LLM returns a single string instead of list
                    data["hallucinations"] = [data["hallucinations"]] if data["hallucinations"] else []

                # Ensure missing_evidence is a list
                if "missing_evidence" not in data or data["missing_evidence"] is None:
                    data["missing_evidence"] = []
                elif isinstance(data["missing_evidence"], str):
                    data["missing_evidence"] = [data["missing_evidence"]] if data["missing_evidence"] else []

                # Ensure reasoning has a default
                if "reasoning" not in data or data["reasoning"] is None:
                    data["reasoning"] = "No reasoning provided by skeptic"

                result = SkepticAuditResult(**data)

                # Record result attributes for observability
                span.set_attribute("skeptic.is_verified", result.is_verified)
                span.set_attribute("skeptic.hallucination_count", len(result.hallucinations))
                span.set_attribute("skeptic.result", "verified" if result.is_verified else "hallucinations_found")
                span.set_status(Status(StatusCode.OK))

                return result

            except (json.JSONDecodeError, KeyError, TypeError, ValueError) as e:
                # FAIL-SAFE: If we can't parse the skeptic's response, assume the answer
                # is NOT verified. This prevents hallucinations from slipping through.
                logger.error(
                    f"Skeptic JSON parse failed: {e}. "
                    f"Failing safe to is_verified=False. Raw response: {response_str[:200]}"
                )
                span.set_status(Status(StatusCode.ERROR, f"JSON parse failed: {e}"))
                span.set_attribute("skeptic.result", "error_parse_failed")
                span.set_attribute("skeptic.error", str(e)[:200])
                return SkepticAuditResult(
                    is_verified=False,
                    reasoning=f"Verification failed due to parse error: {str(e)[:100]}",
                    hallucinations=["Unable to verify - skeptic response was malformed"],
                    missing_evidence=["Re-run verification with valid JSON output"]
                )
            except Exception as e:
                # Catch-all for any unexpected errors - still fail safe
                logger.exception(f"Unexpected error parsing skeptic response: {e}")
                span.set_status(Status(StatusCode.ERROR, f"Unexpected error: {type(e).__name__}"))
                span.set_attribute("skeptic.result", "error_unexpected")
                span.set_attribute("skeptic.error", str(e)[:200])
                return SkepticAuditResult(
                    is_verified=False,
                    reasoning=f"Verification failed due to unexpected error: {type(e).__name__}",
                    hallucinations=["Unable to verify - unexpected error occurred"],
                    missing_evidence=["Re-run verification"]
                )

    async def _execute_refinement(
        self,
        req: RefinerRequest,
        attempt: int = 1,
        temperature: float | None = None
    ) -> str:
        """
        Calls LLM to rewrite the answer based on the audit.

        This method is instrumented with OpenTelemetry for observability.
        Spans include LLM-specific attributes compatible with Arize Phoenix.

        Parameters
        ----------
        req : RefinerRequest
            The refinement request containing query, draft, and audit result.
        attempt : int
            The current refinement attempt number (1-indexed).
        temperature : float | None
            Optional temperature override for this call. If None, uses
            self.refiner_temperature.

        Returns
        -------
        str
            The refined answer text.
        """
        # Use provided temperature or fall back to instance default
        effective_temp = temperature if temperature is not None else self.refiner_temperature

        with tracer.start_as_current_span("verified_pipeline.refinement") as span:
            span.set_attribute("refinement.attempt", attempt)
            span.set_attribute("query.length", len(req.query))
            span.set_attribute("draft_answer.length", len(req.draft_answer))
            span.set_attribute("hallucination_count", len(req.audit_result.hallucinations) if req.audit_result else 0)

            prompt = self._build_refiner_prompt(req)

            # LLM-specific attributes (Phoenix-compatible)
            span.set_attribute("llm.system", "refiner")
            span.set_attribute("llm.provider", self.llm_backend)
            span.set_attribute("llm.model", self.ollama_model)
            span.set_attribute("llm.prompt.length", len(prompt))
            span.set_attribute("llm.prompt.preview", prompt[:500] + "..." if len(prompt) > 500 else prompt)
            span.set_attribute("llm.temperature", effective_temp)

            # Use refiner-specific temperature
            refined_answer = await self._call_llm(prompt, temperature=effective_temp)

            # Record completion attributes
            span.set_attribute("llm.completion.length", len(refined_answer) if refined_answer else 0)
            span.set_attribute("llm.completion.preview",
                (refined_answer[:500] + "...") if refined_answer and len(refined_answer) > 500 else (refined_answer or ""))
            span.set_status(Status(StatusCode.OK))

            return refined_answer

    # --- 3. The Orchestration Loop (Single Responsibility: Workflow) ---

    async def run(
        self,
        query: str,
        session_id: str | None = None,
        strict_mode: bool = True,
        temperature_overrides: dict | None = None,
        relevant_history: list[dict] | None = None,
        expanded_query: dict | None = None,
        original_query: str | None = None,
        contextual_query: str | None = None
    ) -> tuple[str, list[dict]]:
        """
        Execute the verified RAG pipeline with skeptic/optimist debate.

        This method is fully instrumented with OpenTelemetry for observability.
        The span hierarchy enables drill-down analysis in Jaeger/Phoenix:

            verified_pipeline.run
            ├── verified_pipeline.retrieve
            ├── verified_pipeline.draft_generation
            └── verified_pipeline.verification_loop
                ├── verified_pipeline.skeptic_audit.1
                ├── verified_pipeline.refinement.1
                └── verified_pipeline.skeptic_audit.2

        Parameters
        ----------
        query : str
            The user's query to answer.
        session_id : str | None
            Optional session ID for session-scoped document filtering.
        strict_mode : bool
            If True, only answer from documents (no LLM fallback).
        temperature_overrides : dict | None
            Optional per-request temperature overrides. Keys:
            - "optimist": float (0.0-2.0) for draft generation
            - "skeptic": float (0.0-2.0) for audits
            - "refiner": float (0.0-2.0) for refinement
            Example: {"optimist": 0.7, "skeptic": 0.1}
        relevant_history : list[dict] | None
            Optional conversation history for follow-up context. Each dict:
            - "question": str - previous user query
            - "answer": str - previous AI response
            - "turn_number": int | None - turn number
            - "similarity_score": float | None - relevance score
        expanded_query : dict | None
            P8 expanded query from Go orchestrator. Keys:
            - "original": str - original user query
            - "queries": list[str] - expanded query variations
            - "expanded": bool - whether expansion occurred
        original_query : str | None
            The original query before expansion (for logging/tracing).
        contextual_query : str | None
            Query with history context for embedding (P8).

        Returns
        -------
        tuple[str, list[dict]]
            (answer, sources) tuple.
        """
        # Apply request-level temperature overrides (highest priority)
        optimist_temp = self.optimist_temperature
        skeptic_temp = self.skeptic_temperature
        refiner_temp = self.refiner_temperature

        if temperature_overrides:
            if "optimist" in temperature_overrides:
                optimist_temp = self._parse_temperature(
                    temperature_overrides["optimist"], "request.optimist", self.optimist_temperature
                )
            if "skeptic" in temperature_overrides:
                skeptic_temp = self._parse_temperature(
                    temperature_overrides["skeptic"], "request.skeptic", self.skeptic_temperature
                )
            if "refiner" in temperature_overrides:
                refiner_temp = self._parse_temperature(
                    temperature_overrides["refiner"], "request.refiner", self.refiner_temperature
                )
            logger.info(f"Request-level temperature overrides applied: optimist={optimist_temp}, skeptic={skeptic_temp}, refiner={refiner_temp}")

        with tracer.start_as_current_span("verified_pipeline.run") as root_span:
            # Set root span attributes
            root_span.set_attribute("query.text", query[:200] if query else "")
            root_span.set_attribute("query.length", len(query) if query else 0)
            root_span.set_attribute("session.id", session_id or "anonymous")
            root_span.set_attribute("strict_mode", strict_mode)
            root_span.set_attribute("max_verification_attempts", self.max_verification_attempts)
            root_span.set_attribute("temperature.optimist", optimist_temp)
            root_span.set_attribute("temperature.skeptic", skeptic_temp)
            root_span.set_attribute("temperature.refiner", refiner_temp)

            # A. Retrieve Data
            with tracer.start_as_current_span("verified_pipeline.retrieve") as retrieve_span:
                # Log P8 expansion info
                if expanded_query and expanded_query.get("expanded"):
                    retrieve_span.set_attribute("query.expanded", True)
                    retrieve_span.set_attribute("query.variation_count", len(expanded_query.get("queries", [])))
                    retrieve_span.set_attribute("query.original", original_query or query)
                    logger.info(f"Using expanded query: {expanded_query.get('queries', [query])[:3]}")

                query_vector = await self._get_embedding(query)
                retrieve_span.set_attribute("embedding.dimensions", len(query_vector) if query_vector else 0)

                # This calls the method from reranking.py (the PDR logic)
                initial_docs = await self._search_weaviate_initial(query_vector, session_id)
                retrieve_span.set_attribute("retrieved.initial_count", len(initial_docs))

                # P8: Inject history as pseudo-documents for unified reranking
                if relevant_history:
                    initial_docs_with_history = self._inject_history_as_documents(
                        [{"properties": d["properties"], "metadata": d.get("metadata", {})} for d in initial_docs],
                        relevant_history
                    )
                    retrieve_span.set_attribute("history.injected_count", len(relevant_history))
                    logger.info(f"Injected {len(relevant_history)} history turns into document pool")
                else:
                    initial_docs_with_history = initial_docs

                # Use expanded query for reranking if available (P8)
                rerank_query = query
                if expanded_query and expanded_query.get("queries"):
                    rerank_query = expanded_query["queries"][0]
                    retrieve_span.set_attribute("rerank.query", rerank_query[:100])

                context_docs = await self._rerank_docs(rerank_query, initial_docs_with_history)
                retrieve_span.set_attribute("retrieved.reranked_count", len(context_docs))

                # P8: Apply relevance gate to prevent hallucination on garbage retrieval
                has_history = bool(relevant_history)
                context_docs, gate_passed, gate_message = self._check_relevance_gate(
                    context_docs, has_history=has_history
                )
                retrieve_span.set_attribute("relevance_gate.passed", gate_passed)

                if not gate_passed and gate_message:
                    # Gate failed - return low-relevance message instead of hallucinating
                    logger.info("Relevance gate failed, returning low-relevance message")
                    root_span.set_attribute("result", "relevance_gate_failed")
                    root_span.set_status(Status(StatusCode.OK))
                    return gate_message, []

                # Apply relevance threshold filtering in strict mode
                if strict_mode:
                    relevant_docs = [
                        d for d in context_docs
                        if self._has_valid_score(d, threshold=RERANK_SCORE_THRESHOLD)
                    ]
                    retrieve_span.set_attribute("retrieved.relevant_count", len(relevant_docs))
                    logger.info(f"Strict mode: {len(relevant_docs)} of {len(context_docs)} docs above threshold {RERANK_SCORE_THRESHOLD}")

                    if not relevant_docs:
                        logger.info("No relevant documents found in strict mode, returning message")
                        root_span.set_attribute("result", "no_relevant_docs")
                        root_span.set_status(Status(StatusCode.OK))
                        return NO_RELEVANT_DOCS_MESSAGE, []

                    context_docs = relevant_docs

            # Extract properties for prompt usage
            context_props = [d["properties"] for d in context_docs]
            if not context_props:
                root_span.set_attribute("result", "no_context")
                root_span.set_status(Status(StatusCode.OK))
                return NO_RELEVANT_DOCS_MESSAGE, []

            root_span.set_attribute("context.doc_count", len(context_props))

            # B. Initial "Optimist" Draft (P4-3: Enhanced optimist prompt)
            with tracer.start_as_current_span("verified_pipeline.draft_generation") as draft_span:
                draft_prompt = self._build_optimist_prompt(query, context_props, relevant_history)
                draft_span.set_attribute("llm.system", "optimist")
                draft_span.set_attribute("llm.provider", self.llm_backend)
                draft_span.set_attribute("llm.model", self.ollama_model)
                draft_span.set_attribute("llm.prompt.length", len(draft_prompt))
                draft_span.set_attribute("llm.temperature", optimist_temp)

                # Use optimist-specific temperature (supports request-level override)
                initial_answer = await self._call_llm(draft_prompt, temperature=optimist_temp)
                original_draft = initial_answer

                draft_span.set_attribute("llm.completion.length", len(initial_answer) if initial_answer else 0)
                draft_span.set_status(Status(StatusCode.OK))

            # C. Initialize State
            state = VerificationState(current_answer=initial_answer)
            evidence_str = self._format_evidence(context_props)

            # D. Verification Loop
            with tracer.start_as_current_span("verified_pipeline.verification_loop") as loop_span:
                loop_span.set_attribute("evidence.length", len(evidence_str))
                loop_span.set_attribute("max_attempts", self.max_verification_attempts)

                stall_count = 0  # Track consecutive stalls
                max_stalls = 2   # Exit early if refinement keeps stalling

                while state.attempt_count < self.max_verification_attempts:
                    attempt_num = state.attempt_count + 1
                    logger.info(f"Verification attempt {attempt_num} of {self.max_verification_attempts}")
                    loop_span.set_attribute(f"loop.iteration_{attempt_num}.started", True)

                    # 1. Prepare Request
                    audit_req = SkepticAuditRequest(
                        query=query,
                        proposed_answer=state.current_answer,
                        evidence_text=evidence_str
                    )

                    # 2. Run Skeptic (tracing is inside the method)
                    audit_result = await self._execute_skeptic_scan(
                        audit_req, attempt=attempt_num, temperature=skeptic_temp
                    )
                    state.add_audit(audit_result)

                    # Record iteration result
                    loop_span.set_attribute(f"loop.iteration_{attempt_num}.is_verified", audit_result.is_verified)
                    loop_span.set_attribute(f"loop.iteration_{attempt_num}.hallucination_count", len(audit_result.hallucinations))

                    # 3. Check Verdict
                    if audit_result.is_verified:
                        logger.info(f"Answer verified on attempt {attempt_num}")
                        state.mark_verified()
                        break

                    # 4. Refine if more attempts remain
                    if state.attempt_count < self.max_verification_attempts:
                        logger.info(f"Refining to address hallucinations: {audit_result.hallucinations[:3]}...")
                        previous_answer = state.current_answer

                        refine_req = RefinerRequest(
                            query=query,
                            draft_answer=state.current_answer,
                            audit_result=audit_result,
                            evidence_text=evidence_str  # P4-1: Pass evidence to refiner
                        )
                        refined_answer = await self._execute_refinement(
                            refine_req, attempt=attempt_num, temperature=refiner_temp
                        )

                        # Validate the refined answer
                        is_valid, validation_reason = self._validate_refined_answer(refined_answer, previous_answer)
                        loop_span.set_attribute(f"loop.iteration_{attempt_num}.refinement_valid", is_valid)
                        loop_span.set_attribute(f"loop.iteration_{attempt_num}.refinement_reason", validation_reason)

                        if not is_valid:
                            if validation_reason == "stalled_no_change":
                                stall_count += 1
                                logger.warning(f"Refinement stalled ({stall_count}/{max_stalls}): answer unchanged")
                                if stall_count >= max_stalls:
                                    logger.warning("Refinement repeatedly stalled, exiting verification loop early")
                                    loop_span.set_attribute("loop.exit_reason", "refinement_stalled")
                                    break
                            elif validation_reason == "empty_answer":
                                logger.warning("Refinement produced empty answer, keeping previous")
                                loop_span.set_attribute("loop.exit_reason", "empty_refinement")
                                # Keep the previous answer, don't update
                                continue
                            else:
                                logger.warning(f"Refinement validation failed: {validation_reason}")

                        # Update answer if valid (or if invalid but not empty)
                        if is_valid or validation_reason != "empty_answer":
                            state.current_answer = refined_answer
                            stall_count = 0  # Reset stall counter on successful change
                    else:
                        logger.warning(f"Max verification attempts ({self.max_verification_attempts}) reached")
                        loop_span.set_attribute("loop.exit_reason", "max_attempts_reached")

                # Record final loop state
                loop_span.set_attribute("loop.total_iterations", state.attempt_count)
                loop_span.set_attribute("loop.final_verified", state.is_final_verified)
                loop_span.set_status(Status(StatusCode.OK))

            # E. Finalize (inside root span for complete trace coverage)
            final_output = state.current_answer
            if not state.is_final_verified:
                final_output += "\n\n*(Warning: Verification incomplete)*"

            # Format sources using helper methods for consistent metadata access
            sources = [
                {
                    "source": d["properties"].get("source", "Unknown"),
                    "distance": self._get_distance(d),
                    "score": self._get_rerank_score(d),
                } for d in context_docs
            ]

            # Record final result attributes on root span
            root_span.set_attribute("result.is_verified", state.is_final_verified)
            root_span.set_attribute("result.attempt_count", state.attempt_count)
            root_span.set_attribute("result.was_refined", len(state.history) > 1)
            root_span.set_attribute("result.answer_length", len(final_output) if final_output else 0)
            root_span.set_attribute("result.source_count", len(sources))

            # Get trace_id for evaluation correlation (Phoenix/DeepEval/Ragas)
            span_context = root_span.get_span_context()
            trace_id = format(span_context.trace_id, '032x') if span_context.trace_id else None

            if session_id:
                # Pass trace_id for evaluation tool correlation
                await self._log_debate(
                    query, original_draft, state, final_output, session_id, trace_id
                )

            root_span.set_status(Status(StatusCode.OK))
            return final_output, sources

    # --- P6: Run with Progress Callbacks ---

    async def run_with_progress(
        self,
        query: str,
        progress_callback: Callable[[ProgressEvent], Awaitable[None]],
        session_id: str | None = None,
        strict_mode: bool = True,
        temperature_overrides: dict | None = None,
        relevant_history: list[dict] | None = None,
        expanded_query: dict | None = None,
        original_query: str | None = None,
        contextual_query: str | None = None
    ) -> tuple[str, list[dict]]:
        """
        Execute verified RAG pipeline with real-time progress streaming.

        # Description

        This method wraps the standard `run()` logic with progress event
        emissions at each stage of the skeptic/optimist debate. Events are
        streamed via the provided callback, enabling real-time feedback
        to users during the verification process.

        # Parameters

        - query (str): The user's query to answer.
        - progress_callback (Callable[[ProgressEvent], Awaitable[None]]): Async
          callback function that receives progress events. The callback should
          be non-blocking and handle its own error logging.
        - session_id (str | None): Optional session ID for document filtering.
        - strict_mode (bool): If True, only answer from documents.
        - temperature_overrides (dict | None): Per-request temperature overrides.
        - relevant_history (list[dict] | None): Conversation history for context.
        - expanded_query (dict | None): P8 expanded query from Go with keys:
          - "original": str - original user query
          - "queries": list[str] - list of expanded query variations
          - "expanded": bool - whether expansion occurred
        - original_query (str | None): Original query if expanded (for logging).
        - contextual_query (str | None): Query with history context for embedding.

        # Returns

        tuple[str, list[dict]]: (answer, sources) tuple.

        # Examples

            async def my_callback(event: ProgressEvent) -> None:
                await sse_queue.put(event.to_sse_data())

            answer, sources = await pipeline.run_with_progress(
                query="What is Detroit?",
                progress_callback=my_callback
            )

        # Limitations

        - Callback errors are logged but not propagated (fail-safe)
        - Events are emitted at fixed stages; no custom stages supported
        - All events include trace_id for debugging

        # Assumptions

        - Callback completes quickly (non-blocking)
        - Verbosity filtering happens at the handler level, not here
        - All event types are emitted; callback decides what to show
        """
        # Helper to safely emit events (never fail the pipeline on callback error)
        async def emit(event: ProgressEvent) -> None:
            try:
                await progress_callback(event)
            except Exception as e:
                logger.error(f"Progress callback failed for {event.event_type.value}: {e}")

        # Apply request-level temperature overrides
        optimist_temp = self.optimist_temperature
        skeptic_temp = self.skeptic_temperature
        refiner_temp = self.refiner_temperature

        if temperature_overrides:
            if "optimist" in temperature_overrides:
                optimist_temp = self._parse_temperature(
                    temperature_overrides["optimist"], "request.optimist", self.optimist_temperature
                )
            if "skeptic" in temperature_overrides:
                skeptic_temp = self._parse_temperature(
                    temperature_overrides["skeptic"], "request.skeptic", self.skeptic_temperature
                )
            if "refiner" in temperature_overrides:
                refiner_temp = self._parse_temperature(
                    temperature_overrides["refiner"], "request.refiner", self.refiner_temperature
                )
            logger.info(f"Request-level temperature overrides: optimist={optimist_temp}, skeptic={skeptic_temp}, refiner={refiner_temp}")

        with tracer.start_as_current_span("verified_pipeline.run_with_progress") as root_span:
            # Capture trace_id for all events (enables OTel debugging)
            span_context = root_span.get_span_context()
            trace_id = format(span_context.trace_id, '032x') if span_context.trace_id else None

            # Set root span attributes
            root_span.set_attribute("query.text", query[:200] if query else "")
            root_span.set_attribute("query.length", len(query) if query else 0)
            root_span.set_attribute("session.id", session_id or "anonymous")
            root_span.set_attribute("strict_mode", strict_mode)
            root_span.set_attribute("max_verification_attempts", self.max_verification_attempts)
            root_span.set_attribute("temperature.optimist", optimist_temp)
            root_span.set_attribute("temperature.skeptic", skeptic_temp)
            root_span.set_attribute("temperature.refiner", refiner_temp)
            root_span.set_attribute("progress_streaming", True)

            # A. RETRIEVAL PHASE
            await emit(ProgressEvent(
                event_type=ProgressEventType.RETRIEVAL_START,
                message="Retrieving relevant documents...",
                attempt=1,
                trace_id=trace_id
            ))

            with tracer.start_as_current_span("verified_pipeline.retrieve") as retrieve_span:
                # Log P8 expansion info
                if expanded_query and expanded_query.get("expanded"):
                    retrieve_span.set_attribute("query.expanded", True)
                    retrieve_span.set_attribute("query.variation_count", len(expanded_query.get("queries", [])))
                    retrieve_span.set_attribute("query.original", original_query or query)
                    logger.info(f"Using expanded query: {expanded_query.get('queries', [query])[:3]}")

                query_vector = await self._get_embedding(query)
                retrieve_span.set_attribute("embedding.dimensions", len(query_vector) if query_vector else 0)

                initial_docs = await self._search_weaviate_initial(query_vector, session_id)
                retrieve_span.set_attribute("retrieved.initial_count", len(initial_docs))

                # P8: Inject history as pseudo-documents for unified reranking
                # History competes with documents - reranker determines relevance
                if relevant_history:
                    initial_docs_with_history = self._inject_history_as_documents(
                        [{"properties": d["properties"], "metadata": d.get("metadata", {})} for d in initial_docs],
                        relevant_history
                    )
                    retrieve_span.set_attribute("history.injected_count", len(relevant_history))
                    logger.info(f"Injected {len(relevant_history)} history turns into document pool")
                else:
                    initial_docs_with_history = initial_docs

                # Use expanded query for reranking if available
                rerank_query = query
                if expanded_query and expanded_query.get("queries"):
                    # Use the first (most specific) expanded query for reranking
                    rerank_query = expanded_query["queries"][0]
                    retrieve_span.set_attribute("rerank.query", rerank_query[:100])

                context_docs = await self._rerank_docs(rerank_query, initial_docs_with_history)
                retrieve_span.set_attribute("retrieved.reranked_count", len(context_docs))

                # P8: Apply relevance gate to prevent hallucination on garbage retrieval
                has_history = bool(relevant_history)
                context_docs, gate_passed, gate_message = self._check_relevance_gate(
                    context_docs, has_history=has_history
                )
                retrieve_span.set_attribute("relevance_gate.passed", gate_passed)

                if not gate_passed and gate_message:
                    # Gate failed - return low-relevance message instead of hallucinating
                    logger.info("Relevance gate failed, returning low-relevance message")
                    root_span.set_attribute("result", "relevance_gate_failed")
                    root_span.set_status(Status(StatusCode.OK))

                    # Emit completion event for gate failure
                    await emit(ProgressEvent(
                        event_type=ProgressEventType.RETRIEVAL_COMPLETE,
                        message="Retrieved documents below relevance threshold.",
                        attempt=1,
                        trace_id=trace_id,
                        retrieval_details=RetrievalDetails(
                            document_count=0,
                            sources=[],
                            has_relevant_docs=False
                        )
                    ))

                    return gate_message, []

                # Apply relevance threshold in strict mode
                if strict_mode:
                    relevant_docs = [
                        d for d in context_docs
                        if self._has_valid_score(d, threshold=RERANK_SCORE_THRESHOLD)
                    ]
                    retrieve_span.set_attribute("retrieved.relevant_count", len(relevant_docs))
                    logger.info(f"Strict mode: {len(relevant_docs)} of {len(context_docs)} docs above threshold")

                    if not relevant_docs:
                        logger.info("No relevant documents found in strict mode")
                        root_span.set_attribute("result", "no_relevant_docs")
                        root_span.set_status(Status(StatusCode.OK))

                        # Emit completion event with no docs
                        await emit(ProgressEvent(
                            event_type=ProgressEventType.RETRIEVAL_COMPLETE,
                            message="No relevant documents found.",
                            attempt=1,
                            trace_id=trace_id,
                            retrieval_details=RetrievalDetails(
                                document_count=0,
                                sources=[],
                                has_relevant_docs=False
                            )
                        ))

                        return NO_RELEVANT_DOCS_MESSAGE, []

                    context_docs = relevant_docs

            # Extract sources for retrieval complete event
            doc_sources = [
                d.get("properties", {}).get("source", "Unknown")[:100]
                for d in context_docs
            ]

            await emit(ProgressEvent(
                event_type=ProgressEventType.RETRIEVAL_COMPLETE,
                message=f"Retrieved {len(context_docs)} relevant document(s).",
                attempt=1,
                trace_id=trace_id,
                retrieval_details=RetrievalDetails(
                    document_count=len(context_docs),
                    sources=doc_sources,
                    has_relevant_docs=True
                )
            ))

            # Extract properties for prompts
            context_props = [d["properties"] for d in context_docs]
            if not context_props:
                root_span.set_attribute("result", "no_context")
                root_span.set_status(Status(StatusCode.OK))
                return NO_RELEVANT_DOCS_MESSAGE, []

            root_span.set_attribute("context.doc_count", len(context_props))
            evidence_str = self._format_evidence(context_props)

            # B. DRAFT GENERATION PHASE
            await emit(ProgressEvent(
                event_type=ProgressEventType.DRAFT_START,
                message="Generating initial draft answer...",
                attempt=1,
                trace_id=trace_id
            ))

            with tracer.start_as_current_span("verified_pipeline.draft_generation") as draft_span:
                draft_prompt = self._build_optimist_prompt(query, context_props, relevant_history)
                draft_span.set_attribute("llm.system", "optimist")
                draft_span.set_attribute("llm.provider", self.llm_backend)
                draft_span.set_attribute("llm.model", self.ollama_model)
                draft_span.set_attribute("llm.temperature", optimist_temp)

                initial_answer = await self._call_llm(draft_prompt, temperature=optimist_temp)
                original_draft = initial_answer
                draft_span.set_attribute("llm.completion.length", len(initial_answer) if initial_answer else 0)
                draft_span.set_status(Status(StatusCode.OK))

            await emit(ProgressEvent(
                event_type=ProgressEventType.DRAFT_COMPLETE,
                message="Draft generated, sending to skeptic for verification...",
                attempt=1,
                trace_id=trace_id
            ))

            # C. VERIFICATION LOOP
            state = VerificationState(current_answer=initial_answer)
            stall_count = 0
            max_stalls = 2

            with tracer.start_as_current_span("verified_pipeline.verification_loop") as loop_span:
                loop_span.set_attribute("evidence.length", len(evidence_str))
                loop_span.set_attribute("max_attempts", self.max_verification_attempts)

                while state.attempt_count < self.max_verification_attempts:
                    attempt_num = state.attempt_count + 1
                    logger.info(f"Verification attempt {attempt_num} of {self.max_verification_attempts}")
                    loop_span.set_attribute(f"loop.iteration_{attempt_num}.started", True)

                    # D1. SKEPTIC AUDIT
                    await emit(ProgressEvent(
                        event_type=ProgressEventType.SKEPTIC_AUDIT_START,
                        message=f"Skeptic auditing claims (attempt {attempt_num}/{self.max_verification_attempts})...",
                        attempt=attempt_num,
                        trace_id=trace_id
                    ))

                    audit_req = SkepticAuditRequest(
                        query=query,
                        proposed_answer=state.current_answer,
                        evidence_text=evidence_str
                    )

                    audit_result = await self._execute_skeptic_scan(
                        audit_req, attempt=attempt_num, temperature=skeptic_temp
                    )
                    state.add_audit(audit_result)

                    loop_span.set_attribute(f"loop.iteration_{attempt_num}.is_verified", audit_result.is_verified)
                    loop_span.set_attribute(f"loop.iteration_{attempt_num}.hallucination_count", len(audit_result.hallucinations))

                    # Emit skeptic result
                    if audit_result.is_verified:
                        await emit(ProgressEvent(
                            event_type=ProgressEventType.SKEPTIC_AUDIT_COMPLETE,
                            message="✓ All claims verified by evidence!",
                            attempt=attempt_num,
                            trace_id=trace_id,
                            audit_details=SkepticAuditDetails(
                                is_verified=True,
                                reasoning=audit_result.reasoning or "",
                                hallucinations=[],
                                missing_evidence=[],
                                sources_cited=[]
                            )
                        ))
                        logger.info(f"Answer verified on attempt {attempt_num}")
                        state.mark_verified()
                        break
                    else:
                        hallucination_count = len(audit_result.hallucinations)
                        await emit(ProgressEvent(
                            event_type=ProgressEventType.SKEPTIC_AUDIT_COMPLETE,
                            message=f"✗ Found {hallucination_count} unsupported claim(s). Refining...",
                            attempt=attempt_num,
                            trace_id=trace_id,
                            audit_details=SkepticAuditDetails(
                                is_verified=False,
                                reasoning=audit_result.reasoning or "",
                                hallucinations=list(audit_result.hallucinations or []),
                                missing_evidence=list(audit_result.missing_evidence or []),
                                sources_cited=[]
                            )
                        ))

                    # D2. REFINEMENT (if more attempts remain)
                    if state.attempt_count < self.max_verification_attempts:
                        await emit(ProgressEvent(
                            event_type=ProgressEventType.REFINEMENT_START,
                            message=f"Refining answer to remove unsupported claims...",
                            attempt=attempt_num,
                            trace_id=trace_id
                        ))

                        logger.info(f"Refining to address hallucinations: {audit_result.hallucinations[:3]}...")
                        previous_answer = state.current_answer

                        refine_req = RefinerRequest(
                            query=query,
                            draft_answer=state.current_answer,
                            audit_result=audit_result,
                            evidence_text=evidence_str
                        )
                        refined_answer = await self._execute_refinement(
                            refine_req, attempt=attempt_num, temperature=refiner_temp
                        )

                        # Validate the refined answer
                        is_valid, validation_reason = self._validate_refined_answer(refined_answer, previous_answer)
                        loop_span.set_attribute(f"loop.iteration_{attempt_num}.refinement_valid", is_valid)
                        loop_span.set_attribute(f"loop.iteration_{attempt_num}.refinement_reason", validation_reason)

                        if not is_valid:
                            if validation_reason == "stalled_no_change":
                                stall_count += 1
                                logger.warning(f"Refinement stalled ({stall_count}/{max_stalls}): answer unchanged")
                                await emit(ProgressEvent(
                                    event_type=ProgressEventType.REFINEMENT_COMPLETE,
                                    message=f"Refinement stalled ({stall_count}/{max_stalls})",
                                    attempt=attempt_num,
                                    trace_id=trace_id
                                ))
                                if stall_count >= max_stalls:
                                    logger.warning("Refinement repeatedly stalled, exiting early")
                                    loop_span.set_attribute("loop.exit_reason", "refinement_stalled")
                                    break
                            elif validation_reason == "empty_answer":
                                logger.warning("Refinement produced empty answer, keeping previous")
                                await emit(ProgressEvent(
                                    event_type=ProgressEventType.REFINEMENT_COMPLETE,
                                    message="Refinement failed (empty result), keeping previous answer.",
                                    attempt=attempt_num,
                                    trace_id=trace_id
                                ))
                                loop_span.set_attribute("loop.exit_reason", "empty_refinement")
                                continue
                            else:
                                logger.warning(f"Refinement validation failed: {validation_reason}")

                        # Update answer if valid
                        if is_valid or validation_reason != "empty_answer":
                            state.current_answer = refined_answer
                            stall_count = 0

                            await emit(ProgressEvent(
                                event_type=ProgressEventType.REFINEMENT_COMPLETE,
                                message="Refined answer ready for re-verification.",
                                attempt=attempt_num,
                                trace_id=trace_id
                            ))
                    else:
                        logger.warning(f"Max verification attempts ({self.max_verification_attempts}) reached")
                        loop_span.set_attribute("loop.exit_reason", "max_attempts_reached")

                # Record final loop state
                loop_span.set_attribute("loop.total_iterations", state.attempt_count)
                loop_span.set_attribute("loop.final_verified", state.is_final_verified)
                loop_span.set_status(Status(StatusCode.OK))

            # E. FINALIZATION
            final_output = state.current_answer
            if not state.is_final_verified:
                final_output += "\n\n*(Warning: Verification incomplete)*"

            # Format sources
            sources = [
                {
                    "source": d["properties"].get("source", "Unknown"),
                    "distance": self._get_distance(d),
                    "score": self._get_rerank_score(d),
                } for d in context_docs
            ]

            # Emit completion event
            verification_status = "verified" if state.is_final_verified else "unverified"
            await emit(ProgressEvent(
                event_type=ProgressEventType.VERIFICATION_COMPLETE,
                message=f"Verification complete ({verification_status}). Attempts: {state.attempt_count}/{self.max_verification_attempts}.",
                attempt=state.attempt_count,
                trace_id=trace_id
            ))

            # Record final result attributes
            root_span.set_attribute("result.is_verified", state.is_final_verified)
            root_span.set_attribute("result.attempt_count", state.attempt_count)
            root_span.set_attribute("result.was_refined", len(state.history) > 1)
            root_span.set_attribute("result.answer_length", len(final_output) if final_output else 0)
            root_span.set_attribute("result.source_count", len(sources))

            # Log debate for evaluation tools
            if session_id:
                await self._log_debate(
                    query, original_draft, state, final_output, session_id, trace_id
                )

            root_span.set_status(Status(StatusCode.OK))
            return final_output, sources

    async def _log_debate(
        self,
        query: str,
        original_draft: str,
        state: VerificationState,
        final_answer: str,
        session_id: str,
        trace_id: str | None = None
    ):
        """
        Saves the debate transcript to Weaviate for future evaluation (DeepEval/Ragas).

        Parameters
        ----------
        query : str
            The original user query.
        original_draft : str
            The initial "optimist" draft answer before any refinement.
        state : VerificationState
            The verification state containing audit history.
        final_answer : str
            The final answer after verification/refinement.
        session_id : str
            The session ID for grouping related logs.
        trace_id : str | None
            The OpenTelemetry trace ID for correlating with Phoenix/DeepEval/Ragas
            evaluation runs. Format: 32-character hex string.

        Notes
        -----
        This method is defensive against None values and truncates long fields
        to prevent Weaviate storage issues. Failures are logged but do not
        propagate to avoid disrupting the main response flow.

        The trace_id enables powerful evaluation workflows:
        - Phoenix: Filter traces by trace_id to see full LLM call tree
        - DeepEval: Correlate evaluation metrics with specific pipeline runs
        - Ragas: Link faithfulness/relevancy scores to trace spans
        """
        if not self.weaviate_client or not self.weaviate_client.is_connected():
            logger.warning("Weaviate not connected, skipping debate logging.")
            return

        try:
            # Get the last audit result (may be the verified one or last failed one)
            last_audit = state.history[-1] if state.history else None

            # Helper to truncate strings safely
            def safe_truncate(value: str | None, max_len: int = 10000) -> str:
                if value is None:
                    return ""
                s = str(value)
                return s[:max_len] if len(s) > max_len else s

            # Helper to ensure list of strings
            def safe_string_list(items: list | None) -> list[str]:
                if not items:
                    return []
                return [str(item)[:500] for item in items if item is not None]

            # Prepare properties with defensive handling
            properties = {
                "query": safe_truncate(query, 2000),
                "draft_answer": safe_truncate(original_draft),
                "skeptic_critique": safe_truncate(
                    last_audit.reasoning if last_audit else "", 5000
                ),
                "hallucinations_found": safe_string_list(
                    last_audit.hallucinations if last_audit else []
                ),
                "final_answer": safe_truncate(final_answer),
                "was_refined": len(state.history) > 1 if state else False,
                "is_verified": state.is_final_verified if state else False,
                "attempt_count": state.attempt_count if state else 0,
                "session_id": session_id or "anonymous",
                "timestamp": int(time.time() * 1000),
                # OTEL trace_id for evaluation tool correlation
                # Enables linking this debate log to Phoenix/Jaeger traces
                "trace_id": trace_id or ""
            }

            # Insert into Weaviate using asyncio.to_thread to avoid blocking
            verification_logs = self.weaviate_client.collections.get(VERIFICATION_LOG_COLLECTION)

            # Run blocking Weaviate insert in thread pool to not block event loop
            await asyncio.to_thread(
                verification_logs.data.insert,
                properties=properties
            )

            logger.info(
                f"Debate log saved: verified={properties['is_verified']}, "
                f"attempts={properties['attempt_count']}, refined={properties['was_refined']}"
            )

        except (AttributeError, KeyError, TypeError) as e:
            # Specific errors related to data structure issues
            logger.error(f"Failed to prepare debate log properties: {e}", exc_info=True)
        except weaviate.exceptions.WeaviateBaseError as e:
            # Weaviate-specific errors (connection, query, etc.)
            logger.error(f"Weaviate error logging debate: {e}", exc_info=True)
        except Exception as e:
            # Catch-all for unexpected errors - logging shouldn't break response
            logger.error(f"Unexpected error logging debate to Weaviate: {e}", exc_info=True)

    async def retrieve_only(
        self,
        query: str,
        session_id: str | None = None,
        strict_mode: bool = True,
        max_chunks: int = 5
    ) -> tuple[list[dict], str, bool]:
        """
        Retrieval-only mode for the verified pipeline.

        For retrieval-only operations, the verified pipeline delegates to its
        parent RerankingPipeline's implementation since the verification loop
        requires LLM generation (which defeats the purpose of retrieval-only).

        This enables the Go orchestrator to use the verified pipeline's
        retrieval+reranking without triggering the skeptic/optimist debate.

        Parameters
        ----------
        query : str
            The user's query to retrieve documents for.
        session_id : str | None
            The current session ID for session-scoped document filtering.
        strict_mode : bool
            If True, only return documents above relevance threshold.
        max_chunks : int
            Maximum number of document chunks to return.

        Returns
        -------
        tuple[list[dict], str, bool]
            A tuple containing:
            - chunks: List of document dictionaries with content, source, score
            - context_text: Formatted string for LLM context
            - has_relevant_docs: Boolean indicating if relevant documents found

        See Also
        --------
        RerankingPipeline.retrieve_only : Parent implementation with full logic.
        """
        with tracer.start_as_current_span("verified_pipeline.retrieve_only") as span:
            # Input validation
            if not query or not query.strip():
                logger.warning("retrieve_only called with empty query")
                span.set_attribute("error", "empty_query")
                span.set_status(Status(StatusCode.ERROR, "Empty query"))
                return [], "", False

            # Sanitize max_chunks to reasonable bounds
            if max_chunks < 1:
                max_chunks = 1
            elif max_chunks > 20:
                logger.warning(f"max_chunks {max_chunks} exceeds limit, capping at 20")
                max_chunks = 20

            # Set span attributes for observability
            span.set_attribute("query.length", len(query))
            span.set_attribute("query.preview", query[:100] if len(query) > 100 else query)
            span.set_attribute("session.id", session_id or "anonymous")
            span.set_attribute("strict_mode", strict_mode)
            span.set_attribute("max_chunks", max_chunks)
            span.set_attribute("pipeline", "verified")
            span.set_attribute("delegation", "reranking_pipeline")

            try:
                # Delegate to parent implementation
                chunks, context_text, has_relevant = await super().retrieve_only(
                    query, session_id, strict_mode, max_chunks
                )

                # Record results
                span.set_attribute("result.chunk_count", len(chunks))
                span.set_attribute("result.has_relevant_docs", has_relevant)
                span.set_attribute("result.context_length", len(context_text))
                span.set_status(Status(StatusCode.OK))

                return chunks, context_text, has_relevant

            except Exception as e:
                logger.error(f"retrieve_only failed: {e}", exc_info=True)
                span.set_status(Status(StatusCode.ERROR, str(e)[:100]))
                span.set_attribute("error.type", type(e).__name__)
                span.set_attribute("error.message", str(e)[:200])
                # Return safe defaults instead of raising
                return [], "", False
