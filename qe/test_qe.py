"""V3RDE LIF L1: regression tests for the quality-estimator container (stdlib only, no docker).
They keep the L1 fixes in place (docs/sources/L1.md: D3, D4, D6, D8, D9) and the smoke input format."""
import json, os, re, unittest

HERE = os.path.dirname(os.path.abspath(__file__))


def read(name):
    with open(os.path.join(HERE, name), encoding="utf-8") as f:
        return f.read()


class QETest(unittest.TestCase):
    def test_pins(self):
        """D2-D5, D8: every pin that made the image run is still there."""
        d = read("Dockerfile")
        for pin in ('"pyarrow==14.0.2"', '"protobuf==3.20.3"', '"numpy==1.26.4"', '"torch==2.1.2"',
                    '"fsspec==2023.9.2"', '"accelerate==0.20.3"', "fc4978eb064670f7cc33e93ea4f52d38396b8ae6", "FROM python:3.11-slim"):
            self.assertIn(pin, d, pin)

    def test_full_precision_model(self):
        """D9: bf16 matmul fails on the ARM64 CPU; the smoke test must use the fp32 checkpoint."""
        s = read("smoke.sh")
        m = re.search(r'^MODEL="([^"]+)"', s, re.M)
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "google/metricx-24-hybrid-large-v2p6")
        self.assertNotIn("bfloat16", m.group(1))
        self.assertIn("--qe", s)

    def test_smoke_uses_low_memory_wrapper(self):
        """D10: the smoke test runs run_predict.py (low-memory load), and the image contains it."""
        self.assertIn("python /opt/qe/run_predict.py", read("smoke.sh"))
        self.assertNotIn("-m metricx24.predict", read("smoke.sh"))
        self.assertIn("COPY run_predict.py /opt/qe/run_predict.py", read("Dockerfile"))

    def test_low_memory_patch(self):
        """D10: the patch adds low_cpu_mem_usage=True, keeps the class and the other arguments,
        and lets a caller still set the value."""
        import importlib.util
        spec = importlib.util.spec_from_file_location("run_predict", os.path.join(HERE, "run_predict.py"))
        rp = importlib.util.module_from_spec(spec); spec.loader.exec_module(rp)

        class Fake:
            @classmethod
            def from_pretrained(cls, path, **kw):
                return cls, path, kw
        rp.patch_low_memory(Fake)
        cls, path, kw = Fake.from_pretrained("m", torch_dtype="auto")
        self.assertIs(cls, Fake); self.assertEqual(path, "m")
        self.assertEqual(kw, {"torch_dtype": "auto", "low_cpu_mem_usage": True})
        self.assertFalse(Fake.from_pretrained("m", low_cpu_mem_usage=False)[2]["low_cpu_mem_usage"])

    def test_memory_limit_no_swap(self):
        """D6: a hard limit, and --memory-swap equal to --memory (no swap)."""
        s = read("smoke.sh")
        mem = re.search(r"--memory (\S+)", s); swap = re.search(r"--memory-swap (\S+)", s)
        self.assertIsNotNone(mem); self.assertIsNotNone(swap)
        self.assertEqual(mem.group(1), swap.group(1))

    def test_smoke_input(self):
        """D7: six pairs, QE mode (empty reference), every field a string."""
        rows = [json.loads(l) for l in read("smoke_input.jsonl").splitlines() if l.strip()]
        self.assertEqual(len(rows), 6)
        for r in rows:
            self.assertEqual(sorted(r), ["hypothesis", "reference", "source"])
            self.assertEqual(r["reference"], "")
            self.assertTrue(r["source"] and r["hypothesis"])


if __name__ == "__main__":
    unittest.main()
