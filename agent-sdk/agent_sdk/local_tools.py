import subprocess
import re
from pathlib import Path
from typing import Optional


class LocalTools:
    def __init__(self, working_dir: str):
        self.working_dir = Path(working_dir)

    def run_shell(self, cmd: str, timeout: int = 300) -> tuple[str, int]:
        """Run a shell command, return (output, exit_code)."""
        result = subprocess.run(
            cmd, shell=True, capture_output=True, text=True,
            cwd=self.working_dir, timeout=timeout,
        )
        output = result.stdout + result.stderr
        return output, result.returncode

    def read_file(self, path: str, start_line: int = 0, end_line: Optional[int] = None) -> str:
        full_path = self.working_dir / path
        with open(full_path) as f:
            lines = f.readlines()
        if end_line is None:
            end_line = len(lines)
        return "".join(lines[start_line:end_line])

    def write_file(self, path: str, content: str) -> None:
        full_path = self.working_dir / path
        full_path.parent.mkdir(parents=True, exist_ok=True)
        with open(full_path, "w") as f:
            f.write(content)

    def list_directory(self, path: str = ".", max_depth: int = 3) -> str:
        target = self.working_dir / path
        lines: list[str] = []
        self._walk(target, "", max_depth, 0, lines)
        return "\n".join(lines)

    def _walk(self, path: Path, prefix: str, max_depth: int, current_depth: int, lines: list):
        if current_depth >= max_depth:
            return
        try:
            entries = sorted(path.iterdir(), key=lambda e: (not e.is_dir(), e.name))
        except PermissionError:
            return
        for entry in entries:
            if entry.name.startswith("."):
                continue
            lines.append(f"{prefix}{entry.name}{'/' if entry.is_dir() else ''}")
            if entry.is_dir():
                self._walk(entry, prefix + "  ", max_depth, current_depth + 1, lines)

    def search_code(self, pattern: str, glob: str = "**/*") -> list[dict]:
        """Search for pattern in files matching glob."""
        results = []
        for file_path in self.working_dir.glob(glob):
            if file_path.is_file():
                try:
                    content = file_path.read_text(errors="ignore")
                    for i, line in enumerate(content.split("\n"), 1):
                        if re.search(pattern, line):
                            results.append({
                                "file": str(file_path.relative_to(self.working_dir)),
                                "line": i,
                                "content": line.strip(),
                            })
                except Exception:
                    continue
        return results

    def git_commit(self, message: str) -> tuple[str, int]:
        return self.run_shell(f'git add -A && git commit -m "{message}"')

    def git_branch(self, name: str) -> tuple[str, int]:
        return self.run_shell(f"git checkout -b {name}")

    def git_push(self, branch: str) -> tuple[str, int]:
        return self.run_shell(f"git push origin {branch}")

    def git_diff(self) -> tuple[str, int]:
        return self.run_shell("git diff")
