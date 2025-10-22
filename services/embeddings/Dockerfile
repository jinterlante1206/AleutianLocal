FROM python:3.11-slim

LABEL authors="AleutianAI"
WORKDIR /app

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        python3 \
        python3-pip \
        python3-venv \
        git \
        python3-dev \
        build-essential \
    && rm -rf /var/lib/apt/lists/*

RUN python3 -m venv /app/venv
ENV PATH="/app/venv/bin:$PATH"
COPY requirements.txt .
RUN python3 -m pip install --no-cache-dir -r requirements.txt \
    --break-system-packages

COPY server.py .

EXPOSE 12126

CMD ["python3", "server.py"]