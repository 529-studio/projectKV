#!/usr/bin/env python3
"""
validate_labels.py — Strict Draft-07 JSON Schema Validator for Ground Truth Labels.

Part of Sprint 7 Issue #253 (S7-T8).
Validates single label files or entire directories against tools/eval/label_schema.json.
Enforces multi-lane directional structure, red-light queue indicators, and waterline depth.
"""

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
DEFAULT_SCHEMA_PATH = REPO_ROOT / "tools" / "eval" / "label_schema.json"


class SchemaValidationError(Exception):
    """Raised when a label file fails schema validation."""
    pass


def load_schema(schema_path: Path) -> Dict[str, Any]:
    """Loads and returns Draft-07 JSON schema."""
    if not schema_path.exists():
        raise FileNotFoundError(f"Schema file not found: {schema_path}")
    with open(schema_path, "r", encoding="utf-8") as f:
        schema = json.load(f)
    if not isinstance(schema, dict):
        raise ValueError(f"Schema at {schema_path} must be a JSON object")
    return schema


def validate_draft07_builtin(instance: Any, schema: Dict[str, Any], path_prefix: str = "") -> List[str]:
    """
    Validates a JSON instance against a Draft-07 schema without requiring external libraries.
    Checks type, required fields, properties, enums, items, and additionalProperties.
    """
    errors: List[str] = []

    # Check type
    expected_type = schema.get("type")
    if expected_type == "object":
        if not isinstance(instance, dict):
            errors.append(f"{path_prefix or 'root'}: expected object, got {type(instance).__name__}")
            return errors
        
        # Check required fields
        required_fields = schema.get("required", [])
        for field in required_fields:
            if field not in instance:
                field_path = f"{path_prefix}.{field}" if path_prefix else field
                errors.append(f"{field_path}: required field is missing")

        # Check properties
        properties = schema.get("properties", {})
        additional_allowed = schema.get("additionalProperties", True)

        for key, val in instance.items():
            field_path = f"{path_prefix}.{key}" if path_prefix else key
            if key in properties:
                sub_errors = validate_draft07_builtin(val, properties[key], field_path)
                errors.extend(sub_errors)
            elif not additional_allowed:
                errors.append(f"{field_path}: unexpected property not allowed by schema")

    elif expected_type == "array":
        if not isinstance(instance, list):
            errors.append(f"{path_prefix}: expected array, got {type(instance).__name__}")
            return errors
        item_schema = schema.get("items")
        if item_schema and isinstance(item_schema, dict):
            for i, item in enumerate(instance):
                sub_errors = validate_draft07_builtin(item, item_schema, f"{path_prefix}[{i}]")
                errors.extend(sub_errors)

    elif expected_type == "string":
        if not isinstance(instance, str):
            errors.append(f"{path_prefix}: expected string, got {type(instance).__name__}")
            return errors

    elif expected_type == "boolean":
        if not isinstance(instance, bool):
            errors.append(f"{path_prefix}: expected boolean, got {type(instance).__name__}")
            return errors

    elif expected_type == "number" or expected_type == "integer":
        if not isinstance(instance, (int, float)) or (expected_type == "integer" and not isinstance(instance, int)):
            errors.append(f"{path_prefix}: expected {expected_type}, got {type(instance).__name__}")
            return errors

    # Check enum constraint
    if "enum" in schema:
        allowed = schema["enum"]
        if instance not in allowed:
            errors.append(f"{path_prefix}: value '{instance}' is not one of allowed enum values: {allowed}")

    return errors


def validate_label_dict(data: Dict[str, Any], schema: Dict[str, Any]) -> List[str]:
    """Validates label dictionary against schema using jsonschema if available, falling back to builtin."""
    try:
        import jsonschema  # type: ignore
        validator = jsonschema.Draft7Validator(schema)
        errors = []
        for error in validator.iter_errors(data):
            path_str = ".".join(str(p) for p in error.path) or "root"
            errors.append(f"{path_str}: {error.message}")
        return errors
    except ImportError:
        return validate_draft07_builtin(data, schema)


def validate_label_file(file_path: Path, schema: Dict[str, Any]) -> Tuple[bool, List[str]]:
    """Loads a JSON file and validates it against schema."""
    if not file_path.exists():
        return False, [f"File not found: {file_path}"]
    try:
        with open(file_path, "r", encoding="utf-8") as f:
            data = json.load(f)
    except Exception as e:
        return False, [f"Failed to parse JSON: {e}"]

    if not isinstance(data, dict):
        return False, ["Root JSON must be an object"]

    errors = validate_label_dict(data, schema)
    return len(errors) == 0, errors


def find_label_files(target: Path) -> List[Path]:
    """Finds all candidate JSON files in path."""
    if target.is_file():
        return [target]
    elif target.is_dir():
        files = []
        for p in sorted(target.rglob("*.json")):
            if p.name in ("label_schema.json", "schema.json", "package.json", "manifest.json"):
                continue
            files.append(p)
        return files
    return []


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Validate ProjectKV ground-truth label JSON files against Draft-07 schema."
    )
    parser.add_argument(
        "paths",
        nargs="*",
        default=["tools/eval/labels_gold_standard"],
        help="Files or directories containing label JSON files to validate.",
    )
    parser.add_argument(
        "--schema",
        default=str(DEFAULT_SCHEMA_PATH),
        help="Path to Draft-07 label_schema.json file.",
    )
    parser.add_argument(
        "--quiet",
        action="store_true",
        help="Suppress success messages, printing only summary and errors.",
    )
    args = parser.parse_args()

    schema_path = Path(args.schema).resolve()
    try:
        schema = load_schema(schema_path)
    except Exception as e:
        print(f"[ERROR] Failed to load schema from {schema_path}: {e}", file=sys.stderr)
        return 1

    targets: List[Path] = [Path(p).resolve() for p in args.paths]
    all_files: List[Path] = []
    for t in targets:
        all_files.extend(find_label_files(t))

    if not all_files:
        print(f"[WARN] No label files found in targets: {[str(t) for t in targets]}")
        return 0

    total = len(all_files)
    valid_count = 0
    invalid_count = 0
    failures: List[Tuple[Path, List[str]]] = []

    for file_path in all_files:
        is_valid, errors = validate_label_file(file_path, schema)
        if is_valid:
            valid_count += 1
            if not args.quiet:
                print(f"[PASS] {file_path.name}")
        else:
            invalid_count += 1
            failures.append((file_path, errors))
            print(f"[FAIL] {file_path.name}")
            for err in errors:
                print(f"       -> {err}")

    print("\n--- Validation Summary ---")
    print(f"Total files checked: {total}")
    print(f"Valid:   {valid_count} ({(valid_count / total) * 100:.1f}%)")
    print(f"Invalid: {invalid_count}")

    if invalid_count > 0:
        print("\nErrors encountered:")
        for path, errors in failures[:10]:
            print(f"  {path.name}:")
            for err in errors:
                print(f"    - {err}")
        if len(failures) > 10:
            print(f"  ... and {len(failures) - 10} more files.")
        return 1

    print("All label files conform strictly to JSON Schema Draft-07.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
