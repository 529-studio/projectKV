import unittest
from tools.eval.batch_approve_labels import transform_label_payload, is_auto_annotator, compute_approved_key

class TestBatchApproveLabels(unittest.TestCase):
    def test_is_auto_annotator(self):
        self.assertTrue(is_auto_annotator("opencode_auto"))
        self.assertTrue(is_auto_annotator("harvester/auto"))
        self.assertTrue(is_auto_annotator("harvester/rain_trigger"))
        self.assertTrue(is_auto_annotator("auto_harvester"))
        self.assertTrue(is_auto_annotator("auto"))
        self.assertFalse(is_auto_annotator("thdat-bot"))
        self.assertFalse(is_auto_annotator("reviewer"))
        self.assertFalse(is_auto_annotator("thdat"))

    def test_transform_label_payload(self):
        raw = {
            "date": "2026-10-23",
            "camera_id": "cam_10",
            "snapshot_key": "2026-10-23/cam_10/1729600000_sample.jpg",
            "annotator": "opencode_auto",
            "flood_level": "none",
            "traffic_level": "moderate",
            "traffic_dir_a": "moderate",
            "traffic_dir_b": "not_applicable",
            "flood_submersion_anchor": "dry",
            "weather": "clear",
            "time_of_day": "day",
            "notes": "Auto-labeled by opencode_auto",
            "timestamp": 1729600000,
        }
        promoted = transform_label_payload(raw, reviewer="thdat-bot", approved_timestamp=1729700000)
        self.assertEqual(promoted["annotator"], "thdat-bot")
        self.assertEqual(promoted["original_annotator"], "opencode_auto")
        self.assertEqual(promoted["approved_by"], "thdat-bot")
        self.assertEqual(promoted["approved_at"], 1729700000)
        self.assertIn("Approved by thdat-bot", promoted["notes"])
        self.assertEqual(promoted["traffic_level"], "moderate")
        self.assertEqual(promoted["flood_level"], "none")
        self.assertEqual(promoted["flood_submersion_anchor"], "dry")

    def test_compute_approved_key(self):
        old_key = "labels/2026-10-23/cam_10_opencode_auto_1729600000.json"
        new_key = compute_approved_key(old_key, reviewer="thdat-bot")
        self.assertEqual(new_key, "labels/2026-10-23/cam_10_thdat-bot_1729600000.json")

if __name__ == "__main__":
    unittest.main()
