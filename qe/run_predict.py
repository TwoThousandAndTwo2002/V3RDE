"""V3RDE LIF: run MetricX-24's own predict.py, but load the model with low_cpu_mem_usage=True.

Without it, transformers 4.30 builds the model and then loads the full weights into it, so the
load needs about 2x the model size in RAM (fp32 Large: about 9.6 GB), more than the memory the container has.
With it, the peak is about the model size. Only this one argument changes; the input format and
the scoring are Google's code (docs/sources/L1.md D10). Prints the peak RSS at the end.
"""
import os, resource, sys


def patch_low_memory(cls):
    """Wrap cls.from_pretrained (a classmethod) so that low_cpu_mem_usage defaults to True."""
    orig = cls.from_pretrained.__func__

    def from_pretrained(klass, *args, **kwargs):
        kwargs.setdefault("low_cpu_mem_usage", True)
        return orig(klass, *args, **kwargs)

    cls.from_pretrained = classmethod(from_pretrained)
    return cls


def main():
    sys.path.insert(0, os.environ.get("METRICX_DIR", "/opt/metricx"))
    from metricx24 import models, predict   # predict.py does "from . import models": the same module
    patch_low_memory(models.MT5ForRegression)
    predict.main()
    peak_mb = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss / 1024   # Linux: KiB
    sys.stderr.write("peak RSS MB: %d\n" % peak_mb)


if __name__ == "__main__":
    main()
