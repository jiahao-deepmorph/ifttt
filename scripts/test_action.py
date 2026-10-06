"""Exercise the actual Bash launcher with local release assets and a curl stub."""
import hashlib
import io
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().with_name("run-action.sh")


class ActionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="ifttt-action-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "action-version").write_text("v1.2.3\n")
        (self.root / "curl").write_text(
            f"#!{sys.executable}\n"
            "import os,sys,shutil\n"
            "from pathlib import Path\n"
            "root=Path(os.environ['IFTTT_TEST_ROOT'])\n"
            "url=next(x for x in sys.argv if x.startswith('https://'))\n"
            "with (root/'urls').open('a') as f:f.write(url+'\\n')\n"
            "shutil.copyfile(root/url.rsplit('/',1)[1],sys.argv[sys.argv.index('-o')+1])\n"
        )
        (self.root / "curl").chmod(0o755)
        self.env = {
            **os.environ,
            "PATH": str(self.root) + os.pathsep + os.environ["PATH"],
            "IFTTT_TEST_ROOT": str(self.root),
            "IFTTT_ACTION_PATH": str(self.root),
            "IFTTT_ACTION_REPOSITORY": "test/ifttt",
            "IFTTT_FAIL_ON_FINDINGS": "false",
            "IFTTT_BASE_REF": "",
            "GITHUB_EVENT_NAME": "pull_request",
            "GITHUB_SERVER_URL": "https://github.com",
            "RUNNER_TEMP": str(self.root),
            "RUNNER_OS": "Linux",
            "RUNNER_ARCH": "X64",
        }
        self.asset("linux_amd64")

    def asset(self, platform):
        filename = f"ifttt_{platform}.tar.gz"
        data = b'#!/usr/bin/env bash\nprintf "%s\\n" "$@" > "$IFTTT_TEST_ROOT/args"\nexit "${IFTTT_TEST_EXIT:-0}"\n'
        with tarfile.open(self.root / filename, "w:gz") as archive:
            info = tarfile.TarInfo("ifttt.exe" if platform.startswith("windows") else "ifttt")
            info.size = len(data)
            info.mode = 0o755
            archive.addfile(info, io.BytesIO(data))
        digest = hashlib.sha256((self.root / filename).read_bytes()).hexdigest()
        (self.root / "checksums.txt").write_text(f"{digest}  {filename}\n")

    def run_action(self):
        return subprocess.run(["bash", str(SCRIPT)], env=self.env, capture_output=True, text=True)

    def test_platforms_and_pinned_version(self):
        for runner, arch, platform in [
            ("Linux", "X64", "linux_amd64"), ("Linux", "ARM64", "linux_arm64"),
            ("macOS", "ARM64", "darwin_arm64"), ("Windows", "X64", "windows_amd64"),
        ]:
            with self.subTest(platform=platform):
                self.env.update(RUNNER_OS=runner, RUNNER_ARCH=arch)
                self.asset(platform)
                result = self.run_action()
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(f"/releases/download/v1.2.3/ifttt_{platform}.tar.gz", (self.root / "urls").read_text())
                self.assertEqual((self.root / "args").read_text().splitlines(), ["check", "--github", "--fail-on-findings=false"])
                self.assertEqual(list(self.root.glob("ifttt.*")), [])

    def test_exit_codes_and_argument_quoting(self):
        self.env.update(IFTTT_BASE_REF="refs/heads/a; touch nope", IFTTT_FAIL_ON_FINDINGS="true")
        for code in (0, 1, 2, 3):
            self.env["IFTTT_TEST_EXIT"] = str(code)
            result = self.run_action()
            self.assertEqual(result.returncode, code, result.stderr)
            self.assertIn("--base=refs/heads/a; touch nope", (self.root / "args").read_text().splitlines())

    def test_checksum_rejected_before_execution(self):
        (self.root / "checksums.txt").write_text("0" * 64 + "  ifttt_linux_amd64.tar.gz\n")
        result = self.run_action()
        self.assertEqual(result.returncode, 3)
        self.assertIn("checksum verification failed", result.stderr)
        self.assertFalse((self.root / "args").exists())

    def test_invalid_inputs(self):
        for key, value in [("GITHUB_EVENT_NAME", "push"), ("IFTTT_FAIL_ON_FINDINGS", "maybe"), ("RUNNER_ARCH", "ARM"), ("IFTTT_ACTION_REPOSITORY", "../escape")]:
            with self.subTest(key=key):
                old = self.env[key]
                self.env[key] = value
                result = self.run_action()
                self.assertEqual(result.returncode, 3, result.stderr)
                self.env[key] = old


if __name__ == "__main__":
    unittest.main()
