package datatypes

import (
	"context"
	"log"
	"log/slog"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate/entities/models"
)

func GetDocumentSchema() *models.Class {
	indexFilterable := new(bool)
	*indexFilterable = true

	return &models.Class{
		Class:       "Document",
		Description: "A document containing text content and its source.",
		Vectorizer:  "none",
		Properties: []*models.Property{
			{
				Name:         "content",
				DataType:     []string{"text"},
				Description:  "The main content of the document.",
				Tokenization: "word",
			},
			{
				Name:            "source",
				DataType:        []string{"text"},
				Description:     "The original file path or source of the document.",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "parent_source",
				DataType:        []string{"text"},
				Description:     "The original parent file",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "version_tag",
				DataType:        []string{"text"},
				Description:     "A version tag (e.g., git hash, 'v1.0') for this document.",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "data_space",
				DataType:        []string{"text"},
				Description:     "Logical data space for segmentation (e.g., 'work', 'personal').",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:            "ingested_at",
				DataType:        []string{"number"}, // <-- CHANGED from "int" to "number" for int64
				Description:     "Timestamp (Unix ms) of when the chunk was ingested.",
				IndexFilterable: indexFilterable,
			},
		},
	}
}

func GetConversationSchema() *models.Class {
	indexFilterable := new(bool)
	*indexFilterable = true

	return &models.Class{
		Class:       "Conversation",
		Description: "A record of a user question and the AI's answer.",
		Vectorizer:  "none",
		Properties: []*models.Property{
			{
				Name:            "session_id",
				DataType:        []string{"text"},
				Description:     "The unique ID for the conversation session.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:         "question",
				DataType:     []string{"text"},
				Description:  "The user's query to the LLM",
				Tokenization: "word",
			},
			{
				Name:         "answer",
				DataType:     []string{"text"},
				Description:  "The LLMs response",
				Tokenization: "word",
			},
			{
				Name:            "timestamp",
				DataType:        []string{"number"}, // <-- CHANGED from "int" to "number"
				Description:     "The timestamp of the conversation action.",
				IndexFilterable: indexFilterable,
			},
			{
				Name:            "inSession",
				DataType:        []string{"Session"}, // This is the cross-reference
				Description:     "A direct graph link to the parent Session object.",
				IndexFilterable: indexFilterable,
			},
		},
	}
}

func GetSessionSchema() *models.Class {
	indexFilterable := new(bool)
	*indexFilterable = true

	return &models.Class{
		Class:       "Session",
		Description: "Metadata for a single conversation session, including a summary",
		Vectorizer:  "none",
		Properties: []*models.Property{
			{
				Name:            "session_id",
				DataType:        []string{"text"},
				Description:     "The unique ID for the conversation session.",
				IndexFilterable: indexFilterable,
				Tokenization:    "field",
			},
			{
				Name:         "summary",
				DataType:     []string{"text"},
				Description:  "A short, LLM-generated summary of the conversation.",
				Tokenization: "word",
			},
			{
				Name:            "timestamp",
				DataType:        []string{"number"},
				Description:     "The timestamp when the session began.",
				IndexFilterable: indexFilterable,
			},
		},
	}
}

func EnsureWeaviateSchema(client *weaviate.Client) {
	// A list of functions that return our schema definitions.
	schemaGetters := []func() *models.Class{
		GetDocumentSchema,
		GetSessionSchema,
		GetConversationSchema,
	}

	for _, getSchema := range schemaGetters {
		class := getSchema()
		slog.Info("Checking schema", "class", class.Class)

		// Check if the class already exists.
		_, err := client.Schema().ClassGetter().WithClassName(class.Class).Do(context.Background())
		if err != nil {
			// If it doesn't exist, the client returns an error. We can now create it.
			slog.Info("Schema not found, creating it...", "class", class.Class)
			err := client.Schema().ClassCreator().WithClass(class).Do(context.Background())
			if err != nil {
				// If we fail to create it, it's a fatal error.
				log.Fatalf("Failed to create schema for class %s: %v", class.Class, err)
			}
			slog.Info("Successfully created schema", "class", class.Class)
		} else {
			slog.Info("Schema already exists", "class", class.Class)
			// TODO: Add logic to check and update properties if they differ?
		}
	}
}
