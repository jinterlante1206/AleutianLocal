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
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type PolicyEngine struct {
	Classifiers []Classification
}

func NewPolicyEngine(patternFilePath string) (*PolicyEngine, error) {
	yamlFile, err := os.ReadFile(patternFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the policy file %s: %w", patternFilePath, err)
	}
	// Parse the YAML into the types struct
	var classificationFile PolicyEngineClassificationFile
	if err := yaml.Unmarshal(yamlFile, &classificationFile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal the policy file %s: %w", patternFilePath, err)
	}

	// Compile the regex patterns for performance and sort by priority
	if err = classificationFile.CompileRegexes(); err != nil {
		return nil, fmt.Errorf("failed to compile a regex %w", err)
	}

	// Sort the classifications from highest to lowest priority
	classificationFile.SortByPriority()

	// Return the fully initialized engine.
	engine := &PolicyEngine{Classifiers: classificationFile.ClassificationPatterns}
	return engine, nil
}

func (e *PolicyEngine) ClassifyData(data []byte) string {
	for _, classifier := range e.Classifiers {
		for _, re := range classifier.CompiledPatterns {
			if re.Match(data) {
				return classifier.Name
			}
		}
	}
	return "public"
}

func (e *PolicyEngine) ScanFileContent(content string) []ScanFinding {
	var findings []ScanFinding
	lines := strings.Split(content, "\n")
	for lineNum, line := range lines {
		for _, classifier := range e.Classifiers {
			for _, pattern := range classifier.Patterns {
				match := pattern.compiledPattern.FindString(line)
				if match != "" {
					finding := ScanFinding{
						LineNumber:         lineNum + 1,
						MatchedContent:     strings.TrimSpace(match),
						ClassificationName: classifier.Name,
						PatternId:          pattern.Id,
						PatternDescription: pattern.Description,
						Confidence:         pattern.Confidence,
					}
					findings = append(findings, finding)
				}
			}
		}
	}
	return findings
}
