package datatypes

import (
	"context"
	"log"
	"log/slog"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate/entities/models"
)

func GetDocumentSchema() *models.Class {
	return &models.Class{
		Class:       "Document",
		Description: "A document containing text content and its source.",
		Vectorizer:  "none",
		Properties: []*models.Property{
			{
				Name:        "content",
				DataType:    []string{"text"},
				Description: "The main content of the document.",
			},
			{
				Name:        "source",
				DataType:    []string{"string"},
				Description: "The original file path or source of the document.",
			},
		},
	}
}

func GetConversationSchema() *models.Class {
	return &models.Class{
		Class:       "Conversation",
		Description: "A record of a user question and the AI's answer.",
		Vectorizer:  "none",
		Properties: []*models.Property{
			{
				Name:        "session_id",
				DataType:    []string{"string"},
				Description: "The unique ID for the conversation session.",
			},
			{
				Name:        "question",
				DataType:    []string{"text"},
				Description: "The user's query to the LLM",
			},
			{
				Name:        "answer",
				DataType:    []string{"text"},
				Description: "The LLMs response",
			},
			{
				Name:        "timestamp",
				DataType:    []string{"int"},
				Description: "The timestamp of the conversation action.",
			},
		},
	}
}

// GetSessionSchema returns the schema definition for the Session class.
func GetSessionSchema() *models.Class {
	return &models.Class{
		Class:       "Session",
		Description: "Metadata for a single conversation session, including a summary",
		Vectorizer:  "none",
		Properties: []*models.Property{
			{
				Name:        "session_id",
				DataType:    []string{"string"},
				Description: "The unique ID for the conversation session.",
			},
			{
				Name:        "summary",
				DataType:    []string{"text"},
				Description: "A short, LLM-generated summary of the conversation.",
			},
			{
				Name:        "timestamp",
				DataType:    []string{"int"},
				Description: "The timestamp when the session began.",
			},
		},
	}
}

func EnsureWeaviateSchema(client *weaviate.Client) {
	// A list of functions that return our schema definitions.
	schemaGetters := []func() *models.Class{
		GetDocumentSchema,
		GetConversationSchema,
		GetSessionSchema,
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
			slog.Info("Schema created successfully", "class", class.Class)
		} else {
			// If it exists, no error is returned.
			slog.Info("Schema already exists", "class", class.Class)
		}
	}
}
