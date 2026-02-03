// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// LocationClaim represents a claim about a symbol at a specific location.
type LocationClaim struct {
	// Symbol is the symbol name being referenced.
	Symbol string

	// Location is the claimed location (package, file, or partial path).
	Location string

	// Attribute is the claimed attribute (field, parameter, etc.).
	// Empty if no specific attribute is claimed.
	Attribute string

	// Position is the character offset in the response.
	Position int

	// Raw is the matched text.
	Raw string
}

// Package-level compiled regexes for location claim extraction.
var (
	// symbolInLocationPattern matches "X in <location>" patterns.
	// "ProcessData in utils", "Config in pkg/server"
	symbolInLocationPattern = regexp.MustCompile(
		`(?i)\b([A-Z][a-zA-Z0-9_]*)\s+in\s+([a-zA-Z][a-zA-Z0-9_/.\-]*)`,
	)

	// locationDotSymbolPattern matches "<location>.X" patterns.
	// "utils.ProcessData", "server.Config"
	locationDotSymbolPattern = regexp.MustCompile(
		`\b([a-z][a-zA-Z0-9_]*(?:/[a-z][a-zA-Z0-9_]*)?)\.([A-Z][a-zA-Z0-9_]*)`,
	)

	// symbolHasFieldPattern matches "X has field Y" or "X contains field Y".
	// "Config has field Name", "Request contains MaxRetries"
	symbolHasFieldPattern = regexp.MustCompile(
		`(?i)\b([A-Z][a-zA-Z0-9_]*)\s+(?:has|contains|includes)\s+(?:(?:a\s+)?field\s+)?([A-Z][a-zA-Z0-9_]*)`,
	)

	// structWithFieldsPattern matches "X struct with fields A, B, C".
	// "Config struct with fields Name, Timeout, MaxRetries"
	structWithFieldsPattern = regexp.MustCompile(
		`(?i)\b([A-Z][a-zA-Z0-9_]*)\s+(?:struct|type)\s+(?:with|has|contains)\s+fields?\s+([A-Z][a-zA-Z0-9_,\s]+)`,
	)

	// theXInYPattern matches "the X (struct|type|function) in Y".
	// "the Config struct in pkg/server"
	theXInYPattern = regexp.MustCompile(
		`(?i)\bthe\s+([A-Z][a-zA-Z0-9_]*)\s+(?:struct|type|function|method)\s+in\s+([a-zA-Z][a-zA-Z0-9_/.\-]*)`,
	)

	// theXInYHasZPattern matches "the X struct in Y has Z".
	// "the Config struct in server has MaxRetries"
	theXInYHasZPattern = regexp.MustCompile(
		`(?i)\bthe\s+([A-Z][a-zA-Z0-9_]*)\s+(?:struct|type)\s+in\s+([a-zA-Z][a-zA-Z0-9_/.\-]*)\s+(?:has|contains|includes)\s+(?:(?:a\s+)?field\s+)?([A-Z][a-zA-Z0-9_]*)`,
	)
)

// CrossContextChecker validates that claims don't mix information from different code locations.
//
// This checker detects:
// - Attribute confusion: fields from one struct attributed to another
// - Location mismatch: symbol described with wrong location
// - Ambiguous references: symbol exists in multiple locations without disambiguation
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type CrossContextChecker struct {
	config *CrossContextCheckerConfig
}

// NewCrossContextChecker creates a new cross-context confusion checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*CrossContextChecker - The configured checker.
func NewCrossContextChecker(config *CrossContextCheckerConfig) *CrossContextChecker {
	if config == nil {
		config = DefaultCrossContextCheckerConfig()
	}
	return &CrossContextChecker{
		config: config,
	}
}

// Name implements Checker.
func (c *CrossContextChecker) Name() string {
	return "cross_context_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts location-qualified claims from the response and validates them
//	against EvidenceIndex.SymbolDetails. Detects when attributes from one
//	location are incorrectly applied to a symbol at a different location.
//
// Thread Safety: Safe for concurrent use.
func (c *CrossContextChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	if input == nil || input.Response == "" {
		return nil
	}

	// Need symbol details to validate
	if input.EvidenceIndex == nil || len(input.EvidenceIndex.SymbolDetails) == 0 {
		return nil
	}

	// Note: We always check location claims even for single-location symbols
	// because a claim like "Config in database" should be flagged if Config
	// only exists in pkg/server. The multi-location check is only for
	// ambiguous reference flagging.

	var violations []Violation

	// Extract location claims from response
	claims := c.extractLocationClaims(input.Response)

	// Limit claims to check
	if c.config.MaxClaimsToCheck > 0 && len(claims) > c.config.MaxClaimsToCheck {
		claims = claims[:c.config.MaxClaimsToCheck]
	}

	// Validate each claim
	for _, claim := range claims {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		vs := c.validateClaim(ctx, claim, input.EvidenceIndex)
		violations = append(violations, vs...)
	}

	return violations
}

// extractLocationClaims extracts all location-qualified claims from the response.
func (c *CrossContextChecker) extractLocationClaims(response string) []LocationClaim {
	var claims []LocationClaim
	seen := make(map[string]bool) // Dedup by symbol+location+attribute

	// Extract "X in <location>" patterns
	if c.config.CheckLocationClaims {
		claims = c.extractSymbolInLocation(response, claims, seen)
		claims = c.extractLocationDotSymbol(response, claims, seen)
		claims = c.extractTheXInY(response, claims, seen)
		// Also extract "the X in Y has Z" which includes both location and attribute
		if c.config.CheckAttributeConfusion {
			claims = c.extractTheXInYHasZ(response, claims, seen)
		}
	}

	// Extract attribute claims
	if c.config.CheckAttributeConfusion {
		claims = c.extractSymbolHasField(response, claims, seen)
		claims = c.extractStructWithFields(response, claims, seen)
	}

	return claims
}

// extractSymbolInLocation extracts "X in <location>" claims.
func (c *CrossContextChecker) extractSymbolInLocation(response string, claims []LocationClaim, seen map[string]bool) []LocationClaim {
	matches := symbolInLocationPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		symbol := response[match[2]:match[3]]
		location := response[match[4]:match[5]]
		raw := response[match[0]:match[1]]

		key := fmt.Sprintf("%s|%s|", symbol, location)
		if seen[key] {
			continue
		}
		seen[key] = true

		claims = append(claims, LocationClaim{
			Symbol:   symbol,
			Location: location,
			Position: match[0],
			Raw:      raw,
		})
	}
	return claims
}

// extractLocationDotSymbol extracts "<location>.X" claims.
func (c *CrossContextChecker) extractLocationDotSymbol(response string, claims []LocationClaim, seen map[string]bool) []LocationClaim {
	matches := locationDotSymbolPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		location := response[match[2]:match[3]]
		symbol := response[match[4]:match[5]]
		raw := response[match[0]:match[1]]

		// Skip common false positives like "fmt.Println"
		if isBuiltinPackagePrefix(location) {
			continue
		}

		key := fmt.Sprintf("%s|%s|", symbol, location)
		if seen[key] {
			continue
		}
		seen[key] = true

		claims = append(claims, LocationClaim{
			Symbol:   symbol,
			Location: location,
			Position: match[0],
			Raw:      raw,
		})
	}
	return claims
}

// extractTheXInY extracts "the X struct in Y" claims.
func (c *CrossContextChecker) extractTheXInY(response string, claims []LocationClaim, seen map[string]bool) []LocationClaim {
	matches := theXInYPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		symbol := response[match[2]:match[3]]
		location := response[match[4]:match[5]]
		raw := response[match[0]:match[1]]

		key := fmt.Sprintf("%s|%s|", symbol, location)
		if seen[key] {
			continue
		}
		seen[key] = true

		claims = append(claims, LocationClaim{
			Symbol:   symbol,
			Location: location,
			Position: match[0],
			Raw:      raw,
		})
	}
	return claims
}

// extractTheXInYHasZ extracts "the X struct in Y has Z" claims.
func (c *CrossContextChecker) extractTheXInYHasZ(response string, claims []LocationClaim, seen map[string]bool) []LocationClaim {
	matches := theXInYHasZPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 8 {
			continue
		}

		symbol := response[match[2]:match[3]]
		location := response[match[4]:match[5]]
		attribute := response[match[6]:match[7]]
		raw := response[match[0]:match[1]]

		key := fmt.Sprintf("%s|%s|%s", symbol, location, attribute)
		if seen[key] {
			continue
		}
		seen[key] = true

		claims = append(claims, LocationClaim{
			Symbol:    symbol,
			Location:  location,
			Attribute: attribute,
			Position:  match[0],
			Raw:       raw,
		})
	}
	return claims
}

// extractSymbolHasField extracts "X has field Y" claims.
func (c *CrossContextChecker) extractSymbolHasField(response string, claims []LocationClaim, seen map[string]bool) []LocationClaim {
	matches := symbolHasFieldPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		symbol := response[match[2]:match[3]]
		attribute := response[match[4]:match[5]]
		raw := response[match[0]:match[1]]

		key := fmt.Sprintf("%s||%s", symbol, attribute)
		if seen[key] {
			continue
		}
		seen[key] = true

		claims = append(claims, LocationClaim{
			Symbol:    symbol,
			Attribute: attribute,
			Position:  match[0],
			Raw:       raw,
		})
	}
	return claims
}

// extractStructWithFields extracts "X struct with fields A, B, C" claims.
func (c *CrossContextChecker) extractStructWithFields(response string, claims []LocationClaim, seen map[string]bool) []LocationClaim {
	matches := structWithFieldsPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		symbol := response[match[2]:match[3]]
		fieldsStr := response[match[4]:match[5]]
		raw := response[match[0]:match[1]]

		// Parse comma-separated fields
		fields := parseFieldList(fieldsStr)
		for _, field := range fields {
			key := fmt.Sprintf("%s||%s", symbol, field)
			if seen[key] {
				continue
			}
			seen[key] = true

			claims = append(claims, LocationClaim{
				Symbol:    symbol,
				Attribute: field,
				Position:  match[0],
				Raw:       raw,
			})
		}
	}
	return claims
}

// validateClaim validates a single location claim against evidence.
func (c *CrossContextChecker) validateClaim(ctx context.Context, claim LocationClaim, idx *EvidenceIndex) []Violation {
	var violations []Violation

	// Look up symbol in SymbolDetails
	symbolInfos, exists := idx.SymbolDetails[claim.Symbol]
	if !exists {
		// Symbol not in evidence - can't validate
		return nil
	}

	// Case 1: Location specified - validate it matches
	if claim.Location != "" && c.config.CheckLocationClaims {
		matchedInfo := c.matchLocationToSymbolInfo(claim.Location, symbolInfos)
		if matchedInfo == nil {
			// Location doesn't match any known location for this symbol
			// Find what locations actually exist
			actualLocations := c.getLocationList(symbolInfos)
			if len(actualLocations) > 0 {
				RecordCrossContextConfusion(ctx, "location_mismatch", claim.Symbol, claim.Location, actualLocations[0])
				violations = append(violations, Violation{
					Type:           ViolationCrossContextConfusion,
					Severity:       SeverityHigh,
					Code:           "CROSS_CONTEXT_LOCATION_MISMATCH",
					Message:        fmt.Sprintf("'%s' claimed to be in '%s' but is actually in %s", claim.Symbol, claim.Location, formatLocations(actualLocations)),
					Evidence:       claim.Raw,
					Expected:       fmt.Sprintf("'%s' in %s", claim.Symbol, formatLocations(actualLocations)),
					Suggestion:     "Verify the correct location of the symbol in the codebase",
					LocationOffset: claim.Position,
				})
			}
			return violations
		}

		// Location matches - if attribute also specified, validate it
		if claim.Attribute != "" && c.config.CheckAttributeConfusion {
			if !c.attributeExistsInInfo(claim.Attribute, matchedInfo) {
				// Attribute not in matched location - check if it's in another location
				wrongLocation := c.findAttributeInOtherLocation(claim.Attribute, claim.Symbol, matchedInfo, symbolInfos)
				if wrongLocation != "" {
					RecordCrossContextConfusion(ctx, "attribute_confusion", claim.Symbol, claim.Location, wrongLocation)
					violations = append(violations, Violation{
						Type:           ViolationCrossContextConfusion,
						Severity:       SeverityHigh,
						Code:           "CROSS_CONTEXT_ATTRIBUTE_CONFUSION",
						Message:        fmt.Sprintf("'%s.%s' - field '%s' exists in %s, not in %s", claim.Symbol, claim.Attribute, claim.Attribute, wrongLocation, extractFilename(matchedInfo.File)),
						Evidence:       claim.Raw,
						Expected:       fmt.Sprintf("Attribute from correct location"),
						Suggestion:     "Check which version of this symbol has the claimed attribute",
						LocationOffset: claim.Position,
					})
				}
			}
		}
		return violations
	}

	// Case 2: No location specified but attribute claimed
	if claim.Attribute != "" && c.config.CheckAttributeConfusion {
		// Find which locations have this attribute
		locationsWithAttr := c.findLocationsWithAttribute(claim.Attribute, symbolInfos)

		if len(locationsWithAttr) == 0 {
			// Attribute doesn't exist in any location - phantom attribute (handled by AttributeChecker)
			return nil
		}

		if len(locationsWithAttr) > 1 {
			// Attribute exists in multiple locations - ambiguous
			if c.config.FlagAmbiguousReferences && len(symbolInfos) >= c.config.AmbiguityThreshold {
				RecordCrossContextConfusion(ctx, "ambiguous_attribute", claim.Symbol, "", "")
				violations = append(violations, Violation{
					Type:           ViolationCrossContextConfusion,
					Severity:       SeverityWarning,
					Code:           "CROSS_CONTEXT_AMBIGUOUS_ATTRIBUTE",
					Message:        fmt.Sprintf("'%s.%s' is ambiguous - '%s' exists in multiple '%s' definitions: %s", claim.Symbol, claim.Attribute, claim.Attribute, claim.Symbol, formatLocations(locationsWithAttr)),
					Evidence:       claim.Raw,
					Expected:       "Disambiguate by specifying which location",
					Suggestion:     fmt.Sprintf("Specify location, e.g., '%s in %s has %s'", claim.Symbol, locationsWithAttr[0], claim.Attribute),
					LocationOffset: claim.Position,
				})
			}
		}
		// If attribute exists in exactly one location, claim is valid
		return violations
	}

	// Case 3: Symbol reference without location or attribute - check for ambiguity
	if c.config.FlagAmbiguousReferences && len(symbolInfos) >= c.config.AmbiguityThreshold {
		locations := c.getLocationList(symbolInfos)
		RecordCrossContextConfusion(ctx, "ambiguous_reference", claim.Symbol, "", "")
		violations = append(violations, Violation{
			Type:           ViolationCrossContextConfusion,
			Severity:       SeverityInfo,
			Code:           "CROSS_CONTEXT_AMBIGUOUS_REFERENCE",
			Message:        fmt.Sprintf("'%s' exists in %d locations: %s", claim.Symbol, len(symbolInfos), formatLocations(locations)),
			Evidence:       claim.Raw,
			Expected:       "Disambiguate by specifying location",
			Suggestion:     fmt.Sprintf("Specify which '%s', e.g., '%s in %s'", claim.Symbol, claim.Symbol, locations[0]),
			LocationOffset: claim.Position,
		})
	}

	return violations
}

// matchLocationToSymbolInfo finds the SymbolInfo that matches a partial location.
func (c *CrossContextChecker) matchLocationToSymbolInfo(location string, infos []SymbolInfo) *SymbolInfo {
	locationLower := strings.ToLower(location)

	for i := range infos {
		fileLower := strings.ToLower(infos[i].File)

		// Exact match
		if fileLower == locationLower {
			return &infos[i]
		}

		// Partial match - location appears in file path
		if strings.Contains(fileLower, locationLower) {
			return &infos[i]
		}

		// Match by directory/package name
		dir := extractDirectory(infos[i].File)
		if strings.Contains(strings.ToLower(dir), locationLower) {
			return &infos[i]
		}

		// Match by filename without extension
		filename := extractFilename(infos[i].File)
		filenameNoExt := strings.TrimSuffix(filename, ".go")
		if strings.EqualFold(filenameNoExt, location) || strings.Contains(strings.ToLower(filenameNoExt), locationLower) {
			return &infos[i]
		}
	}

	return nil
}

// attributeExistsInInfo checks if an attribute exists in a SymbolInfo.
func (c *CrossContextChecker) attributeExistsInInfo(attr string, info *SymbolInfo) bool {
	attrLower := strings.ToLower(attr)

	// Check struct fields
	for _, field := range info.Fields {
		if strings.EqualFold(field, attr) || strings.ToLower(field) == attrLower {
			return true
		}
	}

	// Check interface methods
	for _, method := range info.Methods {
		if strings.EqualFold(method, attr) || strings.ToLower(method) == attrLower {
			return true
		}
	}

	return false
}

// findAttributeInOtherLocation finds if an attribute exists in a different location for the same symbol.
func (c *CrossContextChecker) findAttributeInOtherLocation(attr, symbol string, excludeInfo *SymbolInfo, allInfos []SymbolInfo) string {
	for _, info := range allInfos {
		// Skip the excluded location
		if info.File == excludeInfo.File {
			continue
		}

		if c.attributeExistsInInfo(attr, &info) {
			return extractFilename(info.File)
		}
	}
	return ""
}

// findLocationsWithAttribute finds all locations where an attribute exists for a symbol.
func (c *CrossContextChecker) findLocationsWithAttribute(attr string, infos []SymbolInfo) []string {
	var locations []string
	for _, info := range infos {
		if c.attributeExistsInInfo(attr, &info) {
			locations = append(locations, extractFilename(info.File))
		}
	}
	return locations
}

// getLocationList returns a list of file locations for symbol infos.
func (c *CrossContextChecker) getLocationList(infos []SymbolInfo) []string {
	locations := make([]string, 0, len(infos))
	for _, info := range infos {
		locations = append(locations, extractFilename(info.File))
	}
	return locations
}

// parseFieldList parses a comma-separated list of field names.
func parseFieldList(fieldsStr string) []string {
	var fields []string
	parts := strings.Split(fieldsStr, ",")
	for _, part := range parts {
		field := strings.TrimSpace(part)
		// Extract just the field name (stop at space or non-identifier char)
		for i, r := range field {
			if r == ' ' || r == '\t' || r == '\n' {
				field = field[:i]
				break
			}
		}
		if field != "" && isValidCrossContextIdentifier(field) {
			fields = append(fields, field)
		}
	}
	return fields
}

// isValidCrossContextIdentifier checks if a string is a valid Go identifier.
func isValidCrossContextIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_') {
				return false
			}
		} else {
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
				return false
			}
		}
	}
	return true
}

// isBuiltinPackagePrefix checks if a location is a builtin Go package prefix.
func isBuiltinPackagePrefix(location string) bool {
	builtins := map[string]bool{
		"fmt": true, "log": true, "os": true, "io": true,
		"strings": true, "strconv": true, "time": true,
		"context": true, "errors": true, "bytes": true,
		"bufio": true, "sort": true, "sync": true,
		"math": true, "regexp": true, "path": true,
		"net": true, "http": true, "json": true,
		"testing": true, "reflect": true, "runtime": true,
	}
	return builtins[strings.ToLower(location)]
}

// extractDirectory extracts the directory path from a file path.
func extractDirectory(path string) string {
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash == -1 {
		return ""
	}
	return path[:lastSlash]
}

// extractFilename extracts the filename from a file path.
func extractFilename(path string) string {
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash == -1 {
		return path
	}
	return path[lastSlash+1:]
}

// formatLocations formats a list of locations for display.
func formatLocations(locations []string) string {
	if len(locations) == 0 {
		return "unknown"
	}
	if len(locations) == 1 {
		return locations[0]
	}
	if len(locations) == 2 {
		return locations[0] + " and " + locations[1]
	}
	return strings.Join(locations[:len(locations)-1], ", ") + ", and " + locations[len(locations)-1]
}
