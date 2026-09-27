import contextlib
import io
import json
import unittest
from unittest.mock import patch

import smoke_workflow as smoke


class SmokeTests(unittest.TestCase):
    def test_request_uses_server_default_tenant_and_scope(self):
        response = io.BytesIO(json.dumps({"status": "ready"}).encode())
        response.status = 200
        with patch.object(smoke, "urlopen", return_value=response) as urlopen:
            self.assertEqual(smoke.request("http://localhost", "/readyz"), {"status": "ready"})
        headers = urlopen.call_args.args[0].headers
        self.assertNotIn("X-tenant-id", headers)
        self.assertNotIn("X-scope-id", headers)

    def run_smoke(self, states, timeout=1):
        responses = [{"status": "ready"}, {"id": "smoke-1"}] + states
        with patch.object(smoke, "request", side_effect=responses) as request:
            with contextlib.redirect_stdout(io.StringIO()):
                result = smoke.smoke("http://localhost", timeout, interval=0.001)
        self.assertEqual(request.call_count, len(responses))
        self.assertEqual(request.call_args_list[1].args[1], "/api/v1/workflows")
        self.assertEqual(request.call_args_list[1].args[2], {"request": smoke.PROMPT})
        return result

    def test_pending_running_to_expected_failure(self):
        state = {"id": "smoke-1", "status": "failed", "error_message": smoke.EXPECTED_ERROR}
        self.assertEqual(self.run_smoke([
            {"id": "smoke-1", "status": "pending"},
            {"id": "smoke-1", "status": "running"}, state]), state)

    def test_unrelated_failure_is_not_success(self):
        with self.assertRaisesRegex(RuntimeError, "unexpected reason"):
            self.run_smoke([{"id": "smoke-1", "status": "failed", "error_message": "database locked"}])

    def test_unexpected_terminal_or_mismatched_id(self):
        for state in [{"id": "smoke-1", "status": "completed"},
                      {"id": "another", "status": "failed"}]:
            with self.subTest(state=state), self.assertRaises(RuntimeError):
                self.run_smoke([state])

    def test_timeout(self):
        with patch.object(smoke.time, "monotonic", side_effect=[0, 0, 0, 2]):
            with self.assertRaises(TimeoutError):
                self.run_smoke([])

    def test_missing_id(self):
        with patch.object(smoke, "request", side_effect=[{}, {}]):
            with self.assertRaisesRegex(RuntimeError, "no workflow id"):
                smoke.smoke("http://localhost")


if __name__ == "__main__":
    unittest.main()
