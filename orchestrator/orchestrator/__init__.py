"""IoT Simulator orchestrator package."""

import sys
from pathlib import Path

# Add gen/python to sys.path so generated proto code is importable
# as 'from simulator.v1 import device_pb2'.
_gen_path = str(Path(__file__).parent.parent / "gen" / "python")
if _gen_path not in sys.path:
    sys.path.insert(0, _gen_path)
