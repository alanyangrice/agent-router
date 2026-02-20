from pathlib import Path
from typing import Optional


class CodeNavigator:
    def __init__(self, working_dir: str):
        self.working_dir = Path(working_dir)

    def get_tree(self, path: str = ".", depth: int = 3) -> str:
        target = self.working_dir / path
        lines: list[str] = []
        self._tree(target, "", depth, 0, lines)
        return "\n".join(lines)

    def _tree(self, path: Path, indent: str, max_depth: int, current: int, lines: list):
        if current >= max_depth or not path.is_dir():
            return
        try:
            entries = sorted(path.iterdir(), key=lambda e: (not e.is_dir(), e.name))
        except PermissionError:
            return
        for entry in entries:
            if entry.name.startswith(".") or entry.name in ("node_modules", "__pycache__", "vendor", ".git"):
                continue
            marker = "/" if entry.is_dir() else ""
            lines.append(f"{indent}{entry.name}{marker}")
            if entry.is_dir():
                self._tree(entry, indent + "  ", max_depth, current + 1, lines)

    def read_file_lines(self, path: str, start: int = 1, end: Optional[int] = None) -> str:
        full_path = self.working_dir / path
        with open(full_path) as f:
            lines = f.readlines()
        start_idx = max(0, start - 1)
        end_idx = end if end else len(lines)
        numbered = []
        for i, line in enumerate(lines[start_idx:end_idx], start=start_idx + 1):
            numbered.append(f"{i:>6}|{line.rstrip()}")
        return "\n".join(numbered)

    def get_file_summary(self, path: str) -> str:
        """Return function/class signatures from a file."""
        full_path = self.working_dir / path
        content = full_path.read_text(errors="ignore")
        lines = content.split("\n")
        signatures = []
        for i, line in enumerate(lines, 1):
            stripped = line.strip()
            if any(stripped.startswith(kw) for kw in ("def ", "class ", "func ", "type ", "interface ", "export ")):
                signatures.append(f"{i:>6}|{line.rstrip()}")
        return "\n".join(signatures) if signatures else "(no signatures found)"

    def list_files(self, path: str = ".", pattern: str = "*") -> list[str]:
        target = self.working_dir / path
        return [
            str(p.relative_to(self.working_dir))
            for p in target.rglob(pattern)
            if p.is_file() and ".git" not in p.parts
        ]
