package datatypes

import (
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
