import json
import os
import subprocess
import time
import sys
import argparse
import re

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.abspath(os.path.join(SCRIPT_DIR, '../../../../../../..'))

SELECTED_FIXTURES_FILE = os.path.join(SCRIPT_DIR, 'tier1_selected_fixtures.json')
RESULTS_FILE = os.path.join(SCRIPT_DIR, 'tier1_mock_results.json')
MAPPING_FILE = os.path.join(SCRIPT_DIR, 'tier1_fixture_mapping.json')
TEST_BINARY = os.path.join(REPO_ROOT, 'bin/e2e.test')

def extract_error_summary(output):
    lines = output.splitlines()
    error_lines = []
    for line in lines:
        l = line.lower()
        if 'error' in l or 'fail' in l or 'rpc error' in l or 'status: false' in l or 'panic:' in l:
            if any(skip in line for skip in ['reflector', 'streamwatcher', 'context canceled', 'client-go', 'test timed out']):
                continue
            error_lines.append(line.strip())
    if error_lines:
        return ' | '.join(error_lines[-3:])[:300]
    return 'Unknown failure (check test logs)'

def run_single_fixture(item, timeout_sec=30):
    kind = item['kind']
    service = item['service']
    fx = item['fixture']

    cmd = [
        TEST_BINARY,
        '-test.v',
        '-test.count=1',
        f'-test.timeout={timeout_sec}s',
        f'-test.run=^TestAllInSeries/fixtures/{fx}$'
    ]
    env = os.environ.copy()
    env['E2E_KUBE_TARGET'] = 'envtest'
    env['RUN_E2E'] = '1'
    env['E2E_GCP_TARGET'] = 'mock'

    start_time = time.time()
    status = 'UNKNOWN'
    error_detail = ''
    try:
        proc = subprocess.run(
            cmd,
            cwd=REPO_ROOT,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=timeout_sec + 5
        )
        duration = time.time() - start_time
        if proc.returncode == 0:
            status = 'PASS'
        else:
            status = 'FAIL'
            error_detail = extract_error_summary(proc.stdout)
    except subprocess.TimeoutExpired as e:
        duration = time.time() - start_time
        status = 'TIMEOUT'
        stdout_str = e.stdout if isinstance(e.stdout, str) else (e.stdout.decode('utf-8', errors='replace') if e.stdout else '')
        error_detail = extract_error_summary(stdout_str)
    except Exception as e:
        duration = time.time() - start_time
        status = 'ERROR'
        error_detail = str(e)

    return {
        'kind': kind,
        'service': service,
        'fixture': fx,
        'status': status,
        'duration': round(duration, 2),
        'error': error_detail
    }

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--limit', type=int, default=30, help='Max fixtures to run in this invocation')
    parser.add_argument('--max-seconds', type=int, default=420, help='Max seconds to run before cleanly stopping')
    parser.add_argument('--reset', action='store_true', help='Reset results file')
    parser.add_argument('--rerun-failed', action='store_true', help='Rerun fixtures that previously failed')
    args = parser.parse_args()

    with open(SELECTED_FIXTURES_FILE) as f:
        fixtures = json.load(f)

    with open(MAPPING_FILE) as f:
        mapping = json.load(f)

    # Load existing results
    results_map = {}
    if os.path.exists(RESULTS_FILE) and not args.reset:
        try:
            with open(RESULTS_FILE) as f:
                saved = json.load(f)
                for r in saved.get('results', []):
                    # Only keep definite PASS or FAIL or NO_FIXTURE
                    if r.get('status') in ('PASS', 'FAIL', 'TIMEOUT'):
                        results_map[r['kind']] = r
        except Exception as e:
            print(f"Warning: could not read existing results: {e}")

    if args.rerun_failed:
        remaining = [fx for fx in fixtures if results_map.get(fx['kind'], {}).get('status') in ('FAIL', 'TIMEOUT', 'ERROR')]
    else:
        remaining = [fx for fx in fixtures if fx['kind'] not in results_map]
    total_tested = len(fixtures)
    already_done = len(results_map)
    print(f"MockGCP Sequential Runner")
    print(f"Total testable fixtures: {total_tested}")
    print(f"Already completed:       {already_done}")
    print(f"Remaining:               {len(remaining)}")
    print(f"This run limit:          {args.limit} fixtures or {args.max_seconds}s\n", flush=True)

    start_global = time.time()
    count_in_run = 0

    for item in remaining:
        if count_in_run >= args.limit:
            print(f"Reached batch limit of {args.limit} fixtures for this invocation.")
            break
        elapsed = time.time() - start_global
        if elapsed >= args.max_seconds:
            print(f"Reached time limit of {args.max_seconds}s for this invocation.")
            break

        res = run_single_fixture(item, timeout_sec=28)
        results_map[item['kind']] = res
        count_in_run += 1
        overall_done = len(results_map)

        status_str = f"[{res['status']}]"
        print(f"[{overall_done:3d}/{total_tested}] {status_str:<9} {item['kind']:<36} ({res['fixture']}) [{res['duration']:4.1f}s] {res['error'][:70]}", flush=True)

        # Save after every fixture
        all_res = list(results_map.values())
        for m in mapping:
            k = m['kind']
            if not m.get('fixtures') and k not in results_map:
                all_res.append({
                    'kind': k,
                    'service': m['service'],
                    'fixture': None,
                    'status': 'NO_FIXTURE',
                    'duration': 0,
                    'error': 'No test fixture found in config/tests/samples/create/'
                })
        passed_cnt = sum(1 for r in all_res if r.get('status') == 'PASS')
        failed_cnt = sum(1 for r in all_res if r.get('status') in ('FAIL', 'TIMEOUT', 'ERROR'))

        with open(RESULTS_FILE, 'w') as f:
            json.dump({
                'results': all_res,
                'passed': passed_cnt,
                'failed': failed_cnt,
                'total_tier1': len(mapping),
                'total_fixtures': total_tested
            }, f, indent=2)

    # Summary
    all_res = list(results_map.values())
    passed_cnt = sum(1 for r in all_res if r.get('status') == 'PASS')
    failed_cnt = sum(1 for r in all_res if r.get('status') in ('FAIL', 'TIMEOUT', 'ERROR'))
    no_fx_cnt = sum(1 for m in mapping if not m.get('fixtures'))

    print(f"\n--- INVOCATION SUMMARY ---")
    print(f"Completed in this run:  {count_in_run}")
    print(f"Total Completed so far: {len(results_map)}/{total_tested}")
    print(f"  PASS:                 {passed_cnt}")
    print(f"  FAIL / TIMEOUT:       {failed_cnt}")
    print(f"  REMAINING:            {total_tested - len(results_map)}")
    if len(results_map) > 0:
        print(f"Current Pass Rate:      {passed_cnt / len(results_map) * 100:.1f}%")

if __name__ == '__main__':
    main()
