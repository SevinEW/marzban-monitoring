import json
import asyncio
import unittest
from online_presence import Observation, XrayPresenceClient, normalize_users, public_snapshot


class PresenceTest(unittest.TestCase):
    def observation(self, users, received=100):
        return Observation(frozenset(users), received, "ok", .2)

    def test_same_account_across_cores_counts_once(self):
        out = public_snapshot({"a": self.observation(["1", "2"]),
                               "b": self.observation(["2", "3"])}, ["a", "b"], 101)
        self.assertEqual(out["users"], 3)
        self.assertEqual(out["nodes"]["a"]["users"], 2)
        self.assertIsNone(out["devices"])

    def test_missing_node_is_not_a_complete_total(self):
        out = public_snapshot({"a": self.observation(["1"])}, ["a", "b"], 101)
        self.assertIsNone(out["users"])
        self.assertEqual(out["observed_users"], 1)
        self.assertIsNone(out["nodes"]["b"]["users"])
        self.assertFalse(out["coverage"]["complete"])

    def test_known_empty_response_can_be_zero(self):
        out = public_snapshot({"a": self.observation([])}, ["a"], 101)
        self.assertEqual(out["users"], 0)
        self.assertTrue(out["coverage"]["complete"])

    def test_unverified_tracking_and_timeout_are_never_zero(self):
        for state in ["tracking_unverified", "unavailable", "timeout"]:
            out = public_snapshot({"a": Observation(status=state)}, ["a"], 101)
            self.assertIsNone(out["users"])
            self.assertIsNone(out["nodes"]["a"]["users"])

    def test_stale_and_clock_anomalies_invalidate_samples(self):
        for now in [103.001, 99]:
            out = public_snapshot({"a": self.observation(["1"])}, ["a"], now)
            self.assertIsNone(out["users"])
            self.assertEqual(out["nodes"]["a"]["status"], "stale")
        self.assertEqual(public_snapshot({"a": self.observation(["1"])}, ["a"], 103)["users"], 1)

    def test_retired_nodes_excluded_and_empty_inventory_unknown(self):
        self.assertIsNone(public_snapshot({}, [], 100)["users"])
        out = public_snapshot({"a": self.observation(["1"]),
                               "retired": self.observation(["9"])}, ["a", "a"], 101)
        self.assertEqual(out["users"], 1)
        self.assertNotIn("retired", out["nodes"])

    def test_counter_format_and_duplicates(self):
        self.assertEqual(normalize_users(["user>>>12>>>online"] * 2), frozenset(["12"]))
        for raw in [[""], ["12"], ["user>>>>>>online"], ["user>>>a>>>b>>>online"],
                    ["user>>>a\n>>>online"], [17], {"users": []}]:
            with self.assertRaises(ValueError):
                normalize_users(raw)

    def test_public_export_does_not_leak_account_identity(self):
        obs = self.observation(["private-account-example"])
        self.assertNotIn("private-account-example", repr(obs))
        self.assertNotIn("private-account-example", json.dumps(public_snapshot({"a": obs}, ["a"], 101)))

    def test_invalid_clocks_cannot_extend_freshness(self):
        for age in [float("nan"), float("inf"), 0, -1]:
            with self.assertRaises(ValueError):
                public_snapshot({}, [], 100, age)
        out = public_snapshot({"a": self.observation(["1"], float("nan"))}, ["a"], 100)
        self.assertIsNone(out["users"])


class GuardTest(unittest.IsolatedAsyncioTestCase):
    async def test_unverified_runtime_never_queries_api(self):
        client = XrayPresenceClient.__new__(XrayPresenceClient)
        self.assertEqual((await client.sample()).status, "tracking_unverified")

    async def test_overlapping_polls_do_not_build_a_queue(self):
        client = XrayPresenceClient.__new__(XrayPresenceClient)
        client.lock = asyncio.Lock()
        async with client.lock:
            self.assertEqual((await client.sample(tracking_verified=True)).status, "busy")


if __name__ == "__main__":
    unittest.main()
