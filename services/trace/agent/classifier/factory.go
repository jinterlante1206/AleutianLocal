// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"fmt"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
)

// ClassifierType specifies which classifier implementation to use.
type ClassifierType string

const (
	// ClassifierTypeRegex uses regex pattern matching (fast, no LLM calls).
	ClassifierTypeRegex ClassifierType = "regex"

	// ClassifierTypeLLM uses LLM-based classification (more accurate, requires LLM calls).
	ClassifierTypeLLM ClassifierType = "llm"
)

// FactoryConfig configures the classifier factory.
type FactoryConfig struct {
	// Type specifies which classifier to create.
	// Defaults to ClassifierTypeRegex if empty.
	Type ClassifierType

	// LLMClient is required when Type is ClassifierTypeLLM.
	// Ignored for ClassifierTypeRegex.
	LLMClient llm.Client

	// ToolDefinitions is required when Type is ClassifierTypeLLM.
	// Ignored for ClassifierTypeRegex.
	ToolDefinitions []tools.ToolDefinition

	// LLMConfig configures the LLM classifier.
	// If nil and Type is ClassifierTypeLLM, DefaultClassifierConfig() is used.
	LLMConfig *ClassifierConfig
}

// NewClassifier creates a QueryClassifier based on the factory configuration.
//
// Description:
//
//	Factory function that creates either a RegexClassifier or LLMClassifier
//	based on the configuration. This allows runtime selection of the
//	classifier implementation.
//
// Inputs:
//
//	config - Factory configuration specifying the classifier type.
//
// Outputs:
//
//	QueryClassifier - The created classifier.
//	error - Non-nil if configuration is invalid or classifier creation fails.
//
// Example:
//
//	// Create regex classifier (default)
//	classifier, err := NewClassifier(FactoryConfig{Type: ClassifierTypeRegex})
//
//	// Create LLM classifier
//	classifier, err := NewClassifier(FactoryConfig{
//	    Type:            ClassifierTypeLLM,
//	    LLMClient:       client,
//	    ToolDefinitions: toolDefs,
//	})
//
// Thread Safety: This function is safe for concurrent use.
func NewClassifier(config FactoryConfig) (QueryClassifier, error) {
	switch config.Type {
	case "", ClassifierTypeRegex:
		return NewRegexClassifier(), nil

	case ClassifierTypeLLM:
		if config.LLMClient == nil {
			return nil, fmt.Errorf("LLMClient is required for LLM classifier")
		}
		if len(config.ToolDefinitions) == 0 {
			return nil, fmt.Errorf("ToolDefinitions is required for LLM classifier")
		}

		llmConfig := DefaultClassifierConfig()
		if config.LLMConfig != nil {
			llmConfig = *config.LLMConfig
		}

		return NewLLMClassifier(config.LLMClient, config.ToolDefinitions, llmConfig)

	default:
		return nil, fmt.Errorf("unknown classifier type: %s", config.Type)
	}
}

// MustNewClassifier creates a QueryClassifier or panics on error.
//
// Description:
//
//	Convenience wrapper around NewClassifier that panics on error.
//	Use only for initialization code where errors are programming errors.
//
// Inputs:
//
//	config - Factory configuration specifying the classifier type.
//
// Outputs:
//
//	QueryClassifier - The created classifier.
//
// Panics:
//
//	If configuration is invalid or classifier creation fails.
//
// Thread Safety: This function is safe for concurrent use.
func MustNewClassifier(config FactoryConfig) QueryClassifier {
	classifier, err := NewClassifier(config)
	if err != nil {
		panic(fmt.Sprintf("failed to create classifier: %v", err))
	}
	return classifier
}
