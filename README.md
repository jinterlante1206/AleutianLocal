# Aleutian Local
## Purpose
The Aleutian Local project focuses on making your local MLOps on your Mac (M2 or better) super 
simple. You can add as many containers as you want and connect them all together. You get 
utilities to convert your local or huggingface hosted models to gguf and quantize them so you 
can run them efficiently on your machine.

## Setup
- `echo "your_hf_token" | podman secret create aleutian_hf_token -` you have to use this command at the start to load 
  your huggingface token into your local podman secrets. The alternative here is to use a .env 
  file, but podman secrets keeps your passwords encrypted at rest unlike the .env.