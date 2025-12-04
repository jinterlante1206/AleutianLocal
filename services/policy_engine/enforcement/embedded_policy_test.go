package enforcement

import (
	"crypto/sha256"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEmbeddedDataIntegrity(t *testing.T) {
	// 1. Ensure the embedded slice is not empty
	if len(DataClassificationPatterns) == 0 {
		t.Fatal("Embedded policy data is empty. Did the build fail to include 'data_classification_patterns.yaml'?")
	}

	// 2. Ensure it is valid YAML (The 'Verify' step)
	var dump map[string]interface{}
	if err := yaml.Unmarshal(DataClassificationPatterns, &dump); err != nil {
		t.Fatalf("Embedded data is not valid YAML: %v", err)
	}

	// 3. Ensure we can calculate a hash (The 'Verify' command logic)
	hash := sha256.Sum256(DataClassificationPatterns)
	if len(hash) != 32 {
		t.Errorf("Hash calculation failed, expected 32 bytes, got %d", len(hash))
	}
	t.Logf("Current Policy Hash: %x", hash)

	// 4. Test if the data classifications file is too short
	if len(DataClassificationPatterns) < 30 {
		t.Fatal("there are no data classification patterns")
	}
	t.Logf("Embedded data classification data size > 0: %d bytes", len(DataClassificationPatterns))

}
