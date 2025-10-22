# Aleutian Local
## Purpose
The Aleutian Local project focuses on making your local MLOps on your Mac (M2 or better) super 
simple. You can add as many containers as you want and connect them all together. You get 
utilities to convert your local or huggingface hosted models to gguf and quantize them so you 
can run them efficiently on your machine.

### Identity
Aleutian is an opinionated but modular MLOps platform designed for developers to rapidly build, deploy, evaluate, and manage sophisticated LLM-native applications, primarily targeting local macOS environments but extensible to hybrid setups.

It acts as a comprehensive MLOps control plane, providing the essential scaffolding (data ingestion, privacy scanning, multi-strategy RAG execution, model conversion, vector storage, session management, evaluation, observability, UI integration) around the user's chosen inference engine. This allows developers to focus on their unique application logic and data, leveraging best-in-class tools for inference (like native Ollama with MPS acceleration) without getting bogged down in complex infrastructure setup.

Key Differentiator: Aleutian embraces a developer-first, extensibility-first approach via podman-compose overrides and clearly defined service interfaces (like the LLMClient), granting full control to integrate custom components (databases, specialized models like TimesFM, unique RAG pipelines) while providing a robust, pre-configured core stack.

## Setup
- `echo "your_hf_token" | podman secret create aleutian_hf_token -` you have to use this command at the start to load 
  your huggingface token into your local podman secrets. The alternative here is to use a .env 
  file, but podman secrets keeps your passwords encrypted at rest unlike the .env.