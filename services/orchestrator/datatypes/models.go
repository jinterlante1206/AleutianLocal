// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.
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
