package llm

import "net/http"

type HFTransformersClient struct {
	httpClient *http.Client
	baseURL    string
}
