# syntax=docker/dockerfile:1
# V3RDE LIF (lost in translation): the translation quality estimator.
# MetricX-24 (Google, Apache-2.0) in reference-free (QE) mode. Sources: docs/sources/L1.md, L2.md
# The layers go from the least often changed to the most often changed (L2 D2), and the
# downloads stay in BuildKit cache mounts, so a rebuilt layer does not download them again (L2 D1, D3).
FROM python:3.11-slim
# The Debian image deletes apt downloads after each install; keep them for the cache mount (L2 D3)
RUN rm -f /etc/apt/apt.conf.d/docker-clean \
 && echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends git build-essential cmake pkg-config
WORKDIR /opt
# The MetricX inference code, pinned to one commit (L1 D2)
RUN git clone https://github.com/google-research/metricx.git \
 && git -C metricx checkout -q fc4978eb064670f7cc33e93ea4f52d38396b8ae6 \
 && git -C metricx log -1 --format="metricx commit %H %cs" | tee /opt/metricx_commit.txt
# The largest download, in its own layer: a change to a small pin does not rebuild it (L2 D2)
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install "torch==2.1.2" "numpy==1.26.4"
WORKDIR /opt/metricx
# MetricX's own pins, plus the pins that let them run today (L1 D3, D4, D5, D8, D10)
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install -r requirements.txt "pyarrow==14.0.2" "protobuf==3.20.3" "numpy==1.26.4" "torch==2.1.2" "fsspec==2023.9.2" "accelerate==0.20.3" \
 && pip list 2>/dev/null | grep -iE "^(torch|transformers|sentencepiece|datasets|pyarrow|numpy|protobuf|fsspec|accelerate) " | tee /opt/pkg_versions.txt
# Loads the model with low_cpu_mem_usage=True, then runs MetricX's predict.py (L1 D10); our code changes most often, so it is last
COPY run_predict.py /opt/qe/run_predict.py
