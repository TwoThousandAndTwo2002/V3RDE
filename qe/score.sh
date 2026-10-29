#!/usr/bin/env bash
# V3RDE LIF L4c: score translation pairs with MetricX-24 in QE mode.
# Usage: qe/score.sh IN.jsonl OUT.jsonl  (rows: source, hypothesis, reference "")
# Scores are 0..25, lower is better. The settings are the same as qe/smoke.sh (L1, L2).
set -euo pipefail
IN="$1"; OUT="$2"
IMG="${QE_IMAGE:-v3rde-qe:L2}"
# fp32, not bf16: bf16 matmul fails in torch on ARM64 CPUs (L1 D9)
MODEL="google/metricx-24-hybrid-large-v2p6"
TOK="google/mt5-large"
# 10 GB hard limit, no swap, in the 12 GiB Colima VM; stop the other containers first (L1 D6)
docker run --rm -i --memory 10g --memory-swap 10g --cpus 6 -v v3rde_qe_hf:/root/.cache/huggingface "$IMG" \
  sh -c "set -e; cat > /tmp/in.jsonl; cd /opt/metricx; python /opt/qe/run_predict.py --tokenizer $TOK --model_name_or_path $MODEL --max_input_length 1536 --batch_size 1 --input_file /tmp/in.jsonl --output_file /tmp/out.jsonl --qe >&2; cat /tmp/out.jsonl" \
  < "$IN" > "$OUT"
