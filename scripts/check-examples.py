#!/usr/bin/env python3

"""
This script validates that all example manifests conform to their corresponding CRDs.
It checks that all required fields are present and that field types match the schema.
"""

import sys
import os
import glob
import json
from pathlib import Path

try:
    import yaml
except ImportError:
    print("Error: PyYAML is not installed. Please install it with: pip install pyyaml", file=sys.stderr)
    print("Or use: apt-get install python3-yaml", file=sys.stderr)
    sys.exit(1)

def load_crds(crd_dir):
    """Load all CRDs from the specified directory."""
    crds = {}
    for crd_file in glob.glob(os.path.join(crd_dir, "*.yaml")):
        with open(crd_file, 'r') as f:
            try:
                docs = yaml.safe_load_all(f)
                for doc in docs:
                    if doc and doc.get('kind') == 'CustomResourceDefinition':
                        crd_name = doc['metadata']['name']
                        # Extract the kind and group from the CRD
                        spec = doc['spec']
                        group = spec['group']
                        for version in spec['versions']:
                            if version.get('served', False):
                                version_name = version['name']
                                kind = spec['names']['kind']
                                key = f"{kind}.{version_name}.{group}"
                                crds[key] = {
                                    'crd': doc,
                                    'file': crd_file,
                                    'schema': version.get('schema', {}).get('openAPIV3Schema', {})
                                }
            except yaml.YAMLError as e:
                print(f"Error parsing CRD file {crd_file}: {e}")
                continue
    return crds

def validate_example(example_file, crds):
    """Validate an example manifest against its corresponding CRD."""
    errors = []
    
    with open(example_file, 'r') as f:
        try:
            docs = yaml.safe_load_all(f)
            for doc_idx, doc in enumerate(docs):
                if not doc:
                    continue
                    
                # Skip non-resource documents
                if 'kind' not in doc or 'apiVersion' not in doc:
                    continue
                
                kind = doc['kind']
                api_version = doc['apiVersion']
                
                # Skip Kubernetes native resources
                if '/' not in api_version:
                    continue
                
                group, version = api_version.rsplit('/', 1)
                key = f"{kind}.{version}.{group}"
                
                # Check if we have a CRD for this resource
                if key not in crds:
                    # Try without version for backward compatibility
                    found = False
                    for crd_key in crds:
                        if crd_key.startswith(f"{kind}.") and crd_key.endswith(f".{group}"):
                            found = True
                            break
                    if not found:
                        errors.append(f"Document {doc_idx}: No CRD found for {kind} in {api_version}")
                    continue
                
                crd_schema = crds[key]['schema']
                if 'properties' in crd_schema:
                    spec_schema = crd_schema.get('properties', {}).get('spec', {})
                    if spec_schema and 'spec' not in doc:
                        errors.append(f"Document {doc_idx}: Missing required 'spec' field")
                    for field in crd_schema.get('required', []):
                        if field not in doc:
                            errors.append(f"Document {doc_idx}: Missing required field '{field}'")
                    spec = doc.get('spec') or {}
                    spec_properties = (spec_schema or {}).get('properties', {})
                    for field_name in ('forProvider', 'initProvider'):
                        if field_name in spec:
                            errors.extend(check_value(
                                spec[field_name],
                                spec_properties.get(field_name, {}),
                                f"Document {doc_idx}: spec.{field_name}",
                            ))
                
        except yaml.YAMLError as e:
            errors.append(f"YAML parsing error: {e}")
        except Exception as e:
            errors.append(f"Unexpected error: {e}")
    
    return errors


def check_value(value, schema, path):
    """Reject fields and types the CRD OpenAPI schema does not allow."""
    errors = []
    if not schema:
        return errors
    expected = schema.get('type')
    if isinstance(expected, list):
        expected = next((item for item in expected if item != 'null'), expected[0])
    if expected == 'object' and not isinstance(value, dict):
        return [f"{path}: expected object"]
    if expected == 'array' and not isinstance(value, list):
        return [f"{path}: expected array"]
    if expected in ('string',) and not isinstance(value, str):
        return [f"{path}: expected string"]
    if expected in ('integer', 'number') and isinstance(value, bool) or (
        expected in ('integer', 'number') and not isinstance(value, (int, float))
    ):
        return [f"{path}: expected number"]
    if expected == 'boolean' and not isinstance(value, bool):
        return [f"{path}: expected boolean"]
    if isinstance(value, dict):
        properties = schema.get('properties', {})
        additional = schema.get('additionalProperties', False)
        for key, child in value.items():
            if key in properties:
                errors.extend(check_value(child, properties[key], f"{path}.{key}"))
            elif additional is True:
                continue
            elif isinstance(additional, dict):
                errors.extend(check_value(child, additional, f"{path}.{key}"))
            elif properties:
                errors.append(f"{path}.{key}: field is not in the CRD schema")
        for key in schema.get('required', []):
            if key not in value:
                errors.append(f"{path}.{key}: required field is missing")
    if isinstance(value, list):
        item_schema = schema.get('items', {})
        for index, child in enumerate(value):
            errors.extend(check_value(child, item_schema, f"{path}[{index}]"))
    return errors

def main():
    if len(sys.argv) < 3:
        print("Usage: check-examples.py <crd-directory> <examples-directory>")
        sys.exit(1)
    
    crd_dir = sys.argv[1]
    examples_dir = sys.argv[2]
    
    if not os.path.isdir(crd_dir):
        print(f"Error: CRD directory '{crd_dir}' does not exist")
        sys.exit(1)
    
    if not os.path.isdir(examples_dir):
        print(f"Error: Examples directory '{examples_dir}' does not exist")
        sys.exit(1)
    
    # Load all CRDs
    print(f"Loading CRDs from {crd_dir}...")
    crds = load_crds(crd_dir)
    print(f"Loaded {len(crds)} CRD versions")
    
    # Find all example files
    example_files = []
    for pattern in ['**/*.yaml', '**/*.yml']:
        example_files.extend(glob.glob(os.path.join(examples_dir, pattern), recursive=True))
    
    print(f"Checking {len(example_files)} example files...")
    
    total_errors = 0
    files_with_errors = []
    
    for example_file in example_files:
        errors = validate_example(example_file, crds)
        if errors:
            total_errors += len(errors)
            files_with_errors.append(example_file)
            print(f"\n❌ {example_file}:")
            for error in errors:
                print(f"  - {error}")
        else:
            print(f"✓ {example_file}")
    
    print(f"\n{'='*60}")
    if total_errors == 0:
        print("✅ All examples validated successfully!")
        sys.exit(0)
    else:
        print(f"❌ Found {total_errors} errors in {len(files_with_errors)} files")
        print("\nFiles with errors:")
        for f in files_with_errors:
            print(f"  - {f}")
        sys.exit(1)

if __name__ == "__main__":
    main()