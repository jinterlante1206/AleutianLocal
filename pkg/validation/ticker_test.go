package validation

import (
	"testing"
)

func TestValidateTicker(t *testing.T) {
	tests := []struct {
		name    string
		ticker  string
		wantErr bool
	}{
		// Valid tickers
		{"simple", "SPY", false},
		{"single char", "A", false},
		{"with digit", "SPY500", false},
		{"class share dot", "BRK.A", false},
		{"class share hyphen", "BF-B", false},
		{"max length", "ABCDEFGHIJ", false},
		{"all digits", "1234567890", false},

		// Invalid tickers - injection attempts
		{"empty", "", true},
		{"injection attempt", `SPY") |> drop()`, true},
		{"sql injection", "SPY'; DROP TABLE--", true},
		{"newline injection", "SPY\n|> drop()", true},
		{"lowercase", "spy", true}, // Must be uppercase
		{"too long", "ABCDEFGHIJK", true},
		{"special chars", "SPY@#$", true},
		{"spaces", "SP Y", true},
		{"unicode", "SPY™", true},
		{"starts with dot", ".SPY", true},
		{"starts with hyphen", "-SPY", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTicker(tt.ticker)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTicker(%q) error = %v, wantErr %v", tt.ticker, err, tt.wantErr)
			}
		})
	}
}

func TestValidateTickers(t *testing.T) {
	tests := []struct {
		name    string
		tickers []string
		wantErr bool
	}{
		{"all valid", []string{"SPY", "QQQ", "AAPL"}, false},
		{"one invalid", []string{"SPY", "bad!", "AAPL"}, true},
		{"all invalid", []string{"spy", "qqq"}, true},
		{"empty slice", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTickers(tt.tickers)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTickers(%v) error = %v, wantErr %v", tt.tickers, err, tt.wantErr)
			}
		})
	}
}

func TestSanitizeTicker(t *testing.T) {
	tests := []struct {
		name    string
		ticker  string
		want    string
		wantErr bool
	}{
		{"uppercase passthrough", "SPY", "SPY", false},
		{"lowercase normalized", "spy", "SPY", false},
		{"mixed case", "SpY", "SPY", false},
		{"with spaces trimmed", "  SPY  ", "SPY", false},
		{"invalid rejected", "bad!", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeTicker(tt.ticker)
			if (err != nil) != tt.wantErr {
				t.Errorf("SanitizeTicker(%q) error = %v, wantErr %v", tt.ticker, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SanitizeTicker(%q) = %q, want %q", tt.ticker, got, tt.want)
			}
		})
	}
}
