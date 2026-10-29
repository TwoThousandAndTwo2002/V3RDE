#!/usr/bin/env bash
# V3RDE LIF L1: smoke test of the quality estimator.
# It checks that the pipeline runs and gives one score per pair. It is NOT an accuracy measure.
# MetricX-24 QE scores are 0..25, lower is better. Rows 3, 5 and 6 are bad on purpose
# (unrelated sentence, undertranslation, untranslated).
set -euo pipefail
cd "$(dirname "$0")"
IMG="${QE_IMAGE:-v3rde-qe:local}"
OUT="${QE_OUT:-${TMPDIR:-/tmp}/v3rde_qe_smoke.jsonl}"
# fp32, not bf16: bf16 matmul fails in torch on ARM64 CPUs (L1 D9)
MODEL="google/metricx-24-hybrid-large-v2p6"
TOK="google/mt5-large"
# 10 GB hard limit, no swap, in the 12 GiB Colima VM; the other containers are stopped for this run (L1 D6)
docker run --rm -i --memory 10g --memory-swap 10g --cpus 6 -v v3rde_qe_hf:/root/.cache/huggingface "$IMG" \
  sh -c "set -e; cat /opt/metricx_commit.txt /opt/pkg_versions.txt >&2; cat > /tmp/in.jsonl; cd /opt/metricx; python /opt/qe/run_predict.py --tokenizer $TOK --model_name_or_path $MODEL --max_input_length 1536 --batch_size 1 --input_file /tmp/in.jsonl --output_file /tmp/out.jsonl --qe >&2; cat /tmp/out.jsonl" \
  < smoke_input.jsonl > "$OUT"
echo "smoke output: $OUT"
