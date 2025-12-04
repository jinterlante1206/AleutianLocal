package datatypes

type ModelPullRequest struct {
	ModelID  string `json:"model_id"`
	Revision string `json:"revision"`
}

type ModelPullResponse struct {
	Status    string `json:"status"`
	ModelID   string `json:"model_id"`
	LocalPath string `json:"local_path"`
	Message   string `json:"message"`
}
