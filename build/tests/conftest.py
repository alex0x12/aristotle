import sys
from pathlib import Path

# Add parent directory to sys.path so tests can import registry module
sys.path.insert(0, str(Path(__file__).parent.parent))
