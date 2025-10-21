// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.
package policy_engine

import (
	"fmt"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

type ConfidenceLevel string

const (
	Low    ConfidenceLevel = "low"
	Medium ConfidenceLevel = "medium"
	High   ConfidenceLevel = "high"
)

type PolicyEngineClassificationFile struct {
	ClassificationPatterns []Classification `yaml:"classifications"`
}

type Classification struct {
	Name             string           `yaml:"name"`
	Description      string           `yaml:"description"`
	Priority         int              `yaml:"priority"`
	Patterns         []Pattern        `yaml:"patterns"`
	CompiledPatterns []*regexp.Regexp `yaml:"-"`
}

type Pattern struct {
	Id              string          `yaml:"id"`
	Description     string          `yaml:"description"`
	Regex           string          `yaml:"regex"`
	Confidence      ConfidenceLevel `yaml:"confidence"`
	compiledPattern *regexp.Regexp  `yaml:"-"`
}

func (c *ConfidenceLevel) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	incomingConfidence := ConfidenceLevel(s)
	switch incomingConfidence {
	case High, Medium, Low:
		*c = incomingConfidence
		return nil
	default:
		return fmt.Errorf("invalid value for Confidence: %q", incomingConfidence)
	}
}

func (p *PolicyEngineClassificationFile) CompileRegexes() error {
	for i := range p.ClassificationPatterns {
		for j := range p.ClassificationPatterns[i].Patterns {
			pattern := &p.ClassificationPatterns[i].Patterns[j]
			re, err := regexp.Compile(pattern.Regex)
			if err != nil {
				return fmt.Errorf("failed to compile the regex %s: %w", pattern.Regex, err)
			}
			p.ClassificationPatterns[i].CompiledPatterns = append(p.ClassificationPatterns[i].
				CompiledPatterns, re)
			pattern.compiledPattern = re
		}
	}
	return nil
}

func (p *PolicyEngineClassificationFile) SortByPriority() {
	sort.Slice(p.ClassificationPatterns, func(i, j int) bool {
		return p.ClassificationPatterns[i].Priority > p.ClassificationPatterns[j].Priority
	})
}

type ScanFinding struct {
	FilePath           string          `json:"file_path"`
	LineNumber         int             `json:"line_number"`
	MatchedContent     string          `json:"matched_content"`
	ClassificationName string          `json:"classification_name"`
	PatternId          string          `json:"pattern_id"`
	PatternDescription string          `json:"pattern_description"`
	Confidence         ConfidenceLevel `json:"confidence"`
	ReviewTimestamp    int64           `json:"review_timestamp"`
	UserDecision       string          `json:"user_decision"`
	Reviewer           string          `json:"reviewer"`
}
