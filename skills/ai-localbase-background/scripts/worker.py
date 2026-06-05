#!/usr/bin/env python3
import argparse
import json
import logging
import os
import re
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, Optional


SKILL_DIR = Path(__file__).resolve().parents[1]
DEFAULT_ENV_FILE = SKILL_DIR / ".env"
WORKER_LOOP_INTERVAL_SECONDS = 1.0
QUEUE_IDLE_EXIT_SECONDS = 3.0


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def print_json(payload: Dict[str, Any]) -> None:
    print(json.dumps(payload, ensure_ascii=False))


def fail(message: str, code: int = 1) -> None:
    print_json({"status": "error", "message": message})
    raise SystemExit(code)


def load_env() -> Dict[str, str]:
    env_file = DEFAULT_ENV_FILE

    if not env_file.exists():
        fail("未找到 .env。请先在 ai-localbase-background skill 当前目录中配置 .env")

    env: Dict[str, str] = {}
    for raw_line in env_file.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        name, value = line.split("=", 1)
        env[name.strip()] = value

    api_base = env.get("MCP_API_BASE_URL", "").strip()
    token = env.get("MCP_AUTH_TOKEN", "").strip()
    if not api_base:
        fail(f"{env_file} 缺少 MCP_API_BASE_URL")
    if not token:
        fail(f"{env_file} 缺少 MCP_AUTH_TOKEN")

    env["_ENV_FILE"] = str(env_file)
    return env


def resolve_workdir(raw_path: str) -> Path:
    if not raw_path:
        return resolve_project_workdir(Path.cwd().resolve())
    return resolve_project_workdir(Path(raw_path).expanduser().resolve())


def is_project_root(path: Path) -> bool:
    if not path.is_dir():
        return False
    markers = (
        "AGENTS.md",
        "agents.md",
        ".git",
        "package.json",
        "composer.json",
        "go.mod",
        "pyproject.toml",
        "README.md",
    )
    return any((path / marker).exists() for marker in markers)


def resolve_project_workdir(workdir: Path) -> Path:
    parts = workdir.parts
    for index in range(len(parts) - 1, 0, -1):
        if parts[index].lower() != "docs":
            continue
        candidate = Path(*parts[:index])
        if is_project_root(candidate):
            # docs 下任务目录只承载阶段文档；知识库和后台状态必须按项目启动目录归属。
            return candidate.resolve()
    return workdir


def resolve_kb_name(workdir: Path) -> str:
    return workdir.name


def runtime_root(workdir: Path) -> Path:
    return workdir / "docs" / ".ai-localbase-background"


def runtime_paths(workdir: Path) -> Dict[str, Path]:
    root = runtime_root(workdir)
    return {
        "root": root,
        "queue": root / "queue",
        "jobs": root / "jobs",
        "results": root / "results",
        "knowledge": root / "knowledge.json",
        "pid": root / "worker.pid",
        "log": root / "worker.log",
    }


def ensure_runtime_dirs(paths: Dict[str, Path]) -> None:
    paths["root"].mkdir(parents=True, exist_ok=True)
    paths["queue"].mkdir(parents=True, exist_ok=True)
    paths["jobs"].mkdir(parents=True, exist_ok=True)
    paths["results"].mkdir(parents=True, exist_ok=True)
    if not paths["knowledge"].exists():
        paths["knowledge"].write_text("{}\n", encoding="utf-8")


def read_json_file(path: Path, default: Any) -> Any:
    if not path.exists():
        return default
    text = path.read_text(encoding="utf-8").strip()
    if not text:
        return default
    return json.loads(text)


def write_json_atomic(path: Path, payload: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp_path = path.with_suffix(path.suffix + ".tmp")
    tmp_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    tmp_path.replace(path)


def read_kb_map(paths: Dict[str, Path]) -> Dict[str, str]:
    knowledge_path = paths["knowledge"]
    try:
        raw = read_json_file(knowledge_path, {})
    except json.JSONDecodeError:
        text = knowledge_path.read_text(encoding="utf-8", errors="replace")
        raw = {}
        for raw_key, raw_value in re.findall(r'"((?:[^"\\]|\\.)+)"\s*:\s*"((?:[^"\\]|\\.)+)"', text):
            try:
                key = json.loads(f'"{raw_key}"')
                value = json.loads(f'"{raw_value}"')
            except json.JSONDecodeError:
                continue
            if isinstance(value, str) and value.startswith("kb-"):
                raw[str(key)] = value
        write_json_atomic(knowledge_path, raw)
    if not isinstance(raw, dict):
        return {}
    result: Dict[str, str] = {}
    for key, value in raw.items():
        result[str(key)] = str(value)
    return result


def write_kb_map(paths: Dict[str, Path], mapping: Dict[str, str]) -> None:
    write_json_atomic(paths["knowledge"], mapping)


def find_nested_value(payload: Any, field_name: str) -> Optional[Any]:
    if isinstance(payload, dict):
        if field_name in payload:
            return payload[field_name]
        for value in payload.values():
            found = find_nested_value(value, field_name)
            if found is not None:
                return found
    if isinstance(payload, list):
        for item in payload:
            found = find_nested_value(item, field_name)
            if found is not None:
                return found
    return None


def post_tool_call(env: Dict[str, str], tool_name: str, arguments_map: Dict[str, Any]) -> Dict[str, Any]:
    url = env["MCP_API_BASE_URL"].rstrip("/") + f"/tools/{tool_name}/call"
    body = json.dumps({"arguments": arguments_map}, ensure_ascii=False).encode("utf-8")
    request = urllib.request.Request(
        url=url,
        data=body,
        method="POST",
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {env['MCP_AUTH_TOKEN']}",
        },
    )

    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            content = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {exc.code}: {detail}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"请求失败: {exc}") from exc

    try:
        return json.loads(content)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"响应不是合法 JSON: {content}") from exc


def post_tools_list(env: Dict[str, str]) -> Dict[str, Any]:
    url = env["MCP_API_BASE_URL"].rstrip("/")
    body = json.dumps(
        {"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}},
        ensure_ascii=False,
    ).encode("utf-8")
    request = urllib.request.Request(
        url=url,
        data=body,
        method="POST",
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {env['MCP_AUTH_TOKEN']}",
        },
    )

    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            content = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {exc.code}: {detail}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"请求失败: {exc}") from exc

    try:
        return json.loads(content)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"响应不是合法 JSON: {content}") from exc


def find_kb_id_by_name(list_response: Dict[str, Any], kb_name: str) -> Optional[str]:
    structured = list_response.get("structuredContent")
    if not isinstance(structured, dict):
        return None
    items = structured.get("items")
    if not isinstance(items, list):
        return None

    for item in items:
        if not isinstance(item, dict):
            continue
        if item.get("name") != kb_name:
            continue
        kb_id = item.get("knowledgeBaseId") or item.get("id")
        if isinstance(kb_id, str) and kb_id:
            return kb_id
    return None


def ensure_kb_id(env: Dict[str, str], workdir: Path, paths: Dict[str, Path]) -> str:
    kb_name = resolve_kb_name(workdir)
    mapping = read_kb_map(paths)

    tools_response = post_tools_list(env)
    if not tools_response:
        raise RuntimeError("读取 tools/list 失败: 响应为空")

    list_response = post_tool_call(env, "knowledge_base.list", {})
    matched_kb_id = find_kb_id_by_name(list_response, kb_name)
    if matched_kb_id:
        mapping[kb_name] = matched_kb_id
        write_kb_map(paths, mapping)
        return matched_kb_id

    response = post_tool_call(
        env,
        "knowledge_base.create",
        {"name": kb_name, "description": f"目录: {workdir}"},
    )
    kb_id = find_nested_value(response, "knowledgeBaseId")
    if not kb_id:
        raise RuntimeError(f"创建知识库失败: {json.dumps(response, ensure_ascii=False)}")

    mapping[kb_name] = str(kb_id)
    write_kb_map(paths, mapping)
    return str(kb_id)


def init_project(env: Dict[str, str], workdir: Path) -> Dict[str, Any]:
    paths = runtime_paths(workdir)
    ensure_runtime_dirs(paths)
    kb_id = ensure_kb_id(env, workdir, paths)
    return {
        "status": "ok",
        "workDir": str(workdir),
        "knowledgeBaseName": resolve_kb_name(workdir),
        "knowledgeBaseId": kb_id,
        "stateDir": str(paths["root"]),
        "envFile": env["_ENV_FILE"],
    }


def is_process_alive(pid: int) -> bool:
    if pid <= 0:
        return False
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def read_pid(paths: Dict[str, Path]) -> Optional[int]:
    if not paths["pid"].exists():
        return None
    content = paths["pid"].read_text(encoding="utf-8").strip()
    if not content:
        return None
    try:
        return int(content)
    except ValueError:
        return None


def worker_status(env: Dict[str, str], workdir: Path) -> Dict[str, Any]:
    paths = runtime_paths(workdir)
    ensure_runtime_dirs(paths)
    pid = read_pid(paths)
    alive = pid is not None and is_process_alive(pid)
    return {
        "status": "ok",
        "workDir": str(workdir),
        "stateDir": str(paths["root"]),
        "workerRunning": alive,
        "pid": pid,
        "logFile": str(paths["log"]),
        "queueSize": len(list(paths["queue"].glob("*.json"))),
    }


def worker_logs(env: Dict[str, str], workdir: Path, lines: int) -> Dict[str, Any]:
    paths = runtime_paths(workdir)
    ensure_runtime_dirs(paths)
    if not paths["log"].exists():
        return {"status": "ok", "logFile": str(paths["log"]), "lines": []}

    all_lines = paths["log"].read_text(encoding="utf-8", errors="replace").splitlines()
    return {
        "status": "ok",
        "logFile": str(paths["log"]),
        "lines": all_lines[-max(lines, 1):],
    }


def stop_worker(env: Dict[str, str], workdir: Path) -> Dict[str, Any]:
    paths = runtime_paths(workdir)
    ensure_runtime_dirs(paths)
    pid = read_pid(paths)
    if pid is None:
        return {"status": "ok", "message": "worker 未运行", "workDir": str(workdir)}

    if not is_process_alive(pid):
        paths["pid"].unlink(missing_ok=True)
        return {"status": "ok", "message": "worker 已停止", "workDir": str(workdir), "pid": pid}

    os.kill(pid, signal.SIGTERM)
    for _ in range(30):
        if not is_process_alive(pid):
            break
        time.sleep(0.1)

    if is_process_alive(pid):
        os.kill(pid, signal.SIGKILL)

    paths["pid"].unlink(missing_ok=True)
    return {"status": "ok", "message": "worker 已停止", "workDir": str(workdir), "pid": pid}


def start_worker(env: Dict[str, str], workdir: Path, idle_exit_seconds: Optional[float] = None) -> Dict[str, Any]:
    paths = runtime_paths(workdir)
    ensure_runtime_dirs(paths)

    existing_pid = read_pid(paths)
    if existing_pid is not None and is_process_alive(existing_pid):
        return {
            "status": "ok",
            "message": "worker 已在运行",
            "workDir": str(workdir),
            "stateDir": str(paths["root"]),
            "pid": existing_pid,
            "logFile": str(paths["log"]),
        }

    log_handle = paths["log"].open("a", encoding="utf-8")
    command = [sys.executable, str(Path(__file__).resolve()), "worker-run", "--workdir", str(workdir)]
    if idle_exit_seconds is not None:
        command.extend(["--idle-exit-seconds", str(idle_exit_seconds)])
    process = subprocess.Popen(
        command,
        cwd=str(workdir),
        stdout=log_handle,
        stderr=subprocess.STDOUT,
        start_new_session=True,
        close_fds=True,
    )
    log_handle.close()

    for _ in range(50):
        if paths["pid"].exists():
            break
        if process.poll() is not None:
            break
        time.sleep(0.1)

    if process.poll() is not None:
        tail = worker_logs(env, workdir, 20)
        raise RuntimeError(f"worker 启动失败: {json.dumps(tail, ensure_ascii=False)}")

    pid = read_pid(paths) or process.pid
    return {
        "status": "ok",
        "message": "worker 启动成功",
        "workDir": str(workdir),
        "stateDir": str(paths["root"]),
        "pid": pid,
        "logFile": str(paths["log"]),
        "idleExitSeconds": idle_exit_seconds,
    }


def new_job_id() -> str:
    return f"job-{int(time.time() * 1000)}"


def enqueue_job(
    action: str,
    env: Dict[str, str],
    workdir: Path,
    payload: Dict[str, Any],
    auto_start_worker: bool = True,
    idle_exit_seconds: float = QUEUE_IDLE_EXIT_SECONDS,
) -> Dict[str, Any]:
    paths = runtime_paths(workdir)
    ensure_runtime_dirs(paths)
    kb_id = ensure_kb_id(env, workdir, paths)

    if action in {"upload", "append", "update"} and not str(payload.get("content", "")).strip():
        fail(f"{action} 任务缺少 content")
    if action in {"append", "update", "delete"} and not str(payload.get("documentId", "")).strip():
        fail(f"{action} 任务缺少 documentId")
    if action == "upload" and not str(payload.get("filename", "")).strip():
        fail("upload 任务缺少 filename")

    job_id = new_job_id()
    job = {
        "jobId": job_id,
        "action": action,
        "workDir": str(workdir),
        "knowledgeBaseId": kb_id,
        "payload": payload,
        "status": "queued",
        "createdAt": now_iso(),
        "updatedAt": now_iso(),
        "attempt": 0,
    }
    queue_path = paths["queue"] / f"{job_id}.json"
    write_json_atomic(queue_path, job)
    result = {
        "status": "ok",
        "jobId": job_id,
        "workDir": str(workdir),
        "queueFile": str(queue_path),
        "knowledgeBaseId": kb_id,
        "workerAutoStart": auto_start_worker,
    }
    if auto_start_worker:
        result["worker"] = start_worker(env, workdir, idle_exit_seconds)
    return result


def locate_job(paths: Dict[str, Path], job_id: str) -> Optional[Path]:
    queue_path = paths["queue"] / f"{job_id}.json"
    if queue_path.exists():
        return queue_path
    job_path = paths["jobs"] / f"{job_id}.json"
    if job_path.exists():
        return job_path
    return None


def get_job_status(workdir: Path, job_id: str) -> Dict[str, Any]:
    paths = runtime_paths(workdir)
    ensure_runtime_dirs(paths)
    job_path = locate_job(paths, job_id)
    if job_path is None:
        fail(f"未找到任务 {job_id}")
    payload = read_json_file(job_path, {})
    payload["jobFile"] = str(job_path)
    return payload


def get_job_result(workdir: Path, job_id: str) -> Dict[str, Any]:
    paths = runtime_paths(workdir)
    ensure_runtime_dirs(paths)
    result_path = paths["results"] / f"{job_id}.json"
    if not result_path.exists():
        status_payload = get_job_status(workdir, job_id)
        return {
            "status": "pending",
            "jobId": job_id,
            "jobStatus": status_payload.get("status"),
            "message": "任务结果尚未生成",
        }
    payload = read_json_file(result_path, {})
    payload["resultFile"] = str(result_path)
    return payload


def build_tool_request(job: Dict[str, Any]) -> Dict[str, Any]:
    action = job["action"]
    kb_id = job["knowledgeBaseId"]
    payload = dict(job["payload"])

    if action == "upload":
        return {
            "tool": "document.upload",
            "arguments": {
                "knowledgeBaseId": kb_id,
                "filename": payload["filename"],
                "content": payload["content"],
            },
        }

    if action == "append":
        return {
            "tool": "document.append",
            "arguments": {
                "knowledgeBaseId": kb_id,
                "documentId": payload["documentId"],
                "content": payload["content"],
            },
        }

    if action == "update":
        return {
            "tool": "document.update",
            "arguments": {
                "knowledgeBaseId": kb_id,
                "documentId": payload["documentId"],
                "content": payload["content"],
            },
        }

    if action == "delete":
        return {
            "tool": "document.delete",
            "arguments": {
                "knowledgeBaseId": kb_id,
                "documentId": payload["documentId"],
            },
        }

    raise RuntimeError(f"当前版本不支持后台动作: {action}")


def run_single_job(env: Dict[str, str], job_path: Path, paths: Dict[str, Path]) -> None:
    job = read_json_file(job_path, {})
    if not isinstance(job, dict):
        raise RuntimeError(f"任务文件格式错误: {job_path}")

    running_path = paths["jobs"] / job_path.name
    job["status"] = "running"
    job["startedAt"] = now_iso()
    job["updatedAt"] = now_iso()
    job["attempt"] = int(job.get("attempt", 0)) + 1
    write_json_atomic(running_path, job)
    job_path.unlink(missing_ok=True)

    try:
        request_data = build_tool_request(job)
        response = post_tool_call(env, request_data["tool"], request_data["arguments"])
        result_payload = {
            "status": "succeeded",
            "jobId": job["jobId"],
            "action": job["action"],
            "workDir": job["workDir"],
            "completedAt": now_iso(),
            "response": response,
        }
        write_json_atomic(paths["results"] / f"{job['jobId']}.json", result_payload)
        job["status"] = "succeeded"
        job["completedAt"] = result_payload["completedAt"]
        job["updatedAt"] = result_payload["completedAt"]
    except Exception as exc:
        completed_at = now_iso()
        result_payload = {
            "status": "failed",
            "jobId": job["jobId"],
            "action": job["action"],
            "workDir": job["workDir"],
            "completedAt": completed_at,
            "error": str(exc),
        }
        write_json_atomic(paths["results"] / f"{job['jobId']}.json", result_payload)
        job["status"] = "failed"
        job["completedAt"] = completed_at
        job["updatedAt"] = completed_at
        job["error"] = str(exc)
    finally:
        write_json_atomic(running_path, job)


def configure_logging(log_file: Path) -> None:
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(message)s",
        handlers=[logging.StreamHandler(sys.stdout)],
        force=True,
    )
    log_file.parent.mkdir(parents=True, exist_ok=True)


def worker_run(env: Dict[str, str], workdir: Path, idle_exit_seconds: Optional[float] = None) -> None:
    paths = runtime_paths(workdir)
    ensure_runtime_dirs(paths)
    configure_logging(paths["log"])
    paths["pid"].write_text(f"{os.getpid()}\n", encoding="utf-8")
    logging.info(
        "worker-started workdir=%s state_dir=%s idle_exit_seconds=%s",
        workdir,
        paths["root"],
        idle_exit_seconds,
    )

    stop_flag = {"value": False}
    idle_started_at: Optional[float] = None

    def _handle_stop(signum: int, frame: Any) -> None:
        stop_flag["value"] = True
        logging.info("worker-stopping signal=%s", signum)

    signal.signal(signal.SIGTERM, _handle_stop)
    signal.signal(signal.SIGINT, _handle_stop)

    while not stop_flag["value"]:
        queue_files = sorted(paths["queue"].glob("*.json"))
        if not queue_files:
            if idle_exit_seconds is not None:
                if idle_started_at is None:
                    idle_started_at = time.monotonic()
                elif time.monotonic() - idle_started_at >= idle_exit_seconds:
                    logging.info("worker-idle-exit idle_exit_seconds=%s", idle_exit_seconds)
                    break
            time.sleep(WORKER_LOOP_INTERVAL_SECONDS)
            continue

        idle_started_at = None
        for queue_file in queue_files:
            if stop_flag["value"]:
                break
            logging.info("job-picked file=%s", queue_file.name)
            run_single_job(env, queue_file, paths)

    paths["pid"].unlink(missing_ok=True)
    logging.info("worker-stopped")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="AI LocalBase background worker")
    subparsers = parser.add_subparsers(dest="command", required=True)

    parser_init = subparsers.add_parser("init")
    parser_init.add_argument("--workdir", required=True)

    parser_worker_start = subparsers.add_parser("worker-start")
    parser_worker_start.add_argument("--workdir", required=True)
    parser_worker_start.add_argument("--idle-exit-seconds", type=float)

    parser_worker_status = subparsers.add_parser("worker-status")
    parser_worker_status.add_argument("--workdir", required=True)

    parser_worker_logs = subparsers.add_parser("worker-logs")
    parser_worker_logs.add_argument("--workdir", required=True)
    parser_worker_logs.add_argument("--lines", type=int, default=50)

    parser_worker_stop = subparsers.add_parser("worker-stop")
    parser_worker_stop.add_argument("--workdir", required=True)

    parser_worker_run = subparsers.add_parser("worker-run")
    parser_worker_run.add_argument("--workdir", required=True)
    parser_worker_run.add_argument("--idle-exit-seconds", type=float)

    parser_queue_upload = subparsers.add_parser("queue-upload")
    parser_queue_upload.add_argument("--filename", required=True)
    parser_queue_upload.add_argument("--content", required=True)
    parser_queue_upload.add_argument("--workdir", required=True)

    parser_queue_append = subparsers.add_parser("queue-append")
    parser_queue_append.add_argument("--document-id", required=True)
    parser_queue_append.add_argument("--content", required=True)
    parser_queue_append.add_argument("--workdir", required=True)

    parser_queue_update = subparsers.add_parser("queue-update")
    parser_queue_update.add_argument("--document-id", required=True)
    parser_queue_update.add_argument("--content", required=True)
    parser_queue_update.add_argument("--workdir", required=True)

    parser_queue_delete = subparsers.add_parser("queue-delete")
    parser_queue_delete.add_argument("--document-id", required=True)
    parser_queue_delete.add_argument("--workdir", required=True)

    parser_job_status = subparsers.add_parser("job-status")
    parser_job_status.add_argument("--job-id", required=True)
    parser_job_status.add_argument("--workdir", required=True)

    parser_job_result = subparsers.add_parser("job-result")
    parser_job_result.add_argument("--job-id", required=True)
    parser_job_result.add_argument("--workdir", required=True)

    return parser


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    workdir = resolve_workdir(getattr(args, "workdir", ""))
    env = load_env()

    if args.command == "init":
        print_json(init_project(env, workdir))
        return

    if args.command == "worker-start":
        print_json(start_worker(env, workdir, args.idle_exit_seconds))
        return

    if args.command == "worker-status":
        print_json(worker_status(env, workdir))
        return

    if args.command == "worker-logs":
        print_json(worker_logs(env, workdir, args.lines))
        return

    if args.command == "worker-stop":
        print_json(stop_worker(env, workdir))
        return

    if args.command == "worker-run":
        worker_run(env, workdir, args.idle_exit_seconds)
        return

    if args.command == "queue-upload":
        print_json(enqueue_job("upload", env, workdir, {"filename": args.filename, "content": args.content}))
        return

    if args.command == "queue-append":
        print_json(enqueue_job("append", env, workdir, {"documentId": args.document_id, "content": args.content}))
        return

    if args.command == "queue-update":
        print_json(enqueue_job("update", env, workdir, {"documentId": args.document_id, "content": args.content}))
        return

    if args.command == "queue-delete":
        print_json(enqueue_job("delete", env, workdir, {"documentId": args.document_id}))
        return

    if args.command == "job-status":
        print_json(get_job_status(workdir, args.job_id))
        return

    if args.command == "job-result":
        print_json(get_job_result(workdir, args.job_id))
        return

    parser.error(f"未知命令: {args.command}")


if __name__ == "__main__":
    main()
