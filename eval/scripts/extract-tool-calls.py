#!/usr/bin/env python3
"""Extract tool calls from Claude stream-json output.

Parses stream-json .jsonl and produces structured JSON with:
- tool_calls: chronological list of {tool, input, result}
- knowledge_queries: what was searched in knowledge base
- knowledge_docs_used: which docs were returned
- import_yaml_generated: any import YAML that was sent
- env_vars_set: env vars that were set per service
- errors: any errors from tool calls
- retries: tool calls that were repeated (same tool+similar input)

Adapted from ../../../eval/scripts/extract-tool-calls.py for ZCP.
"""

import json
import re
import sys


def process_tool_result(tool_id, content, pending_tools, tool_calls,
                        knowledge_docs_used, errors):
    """Process a single tool result and move it from pending to completed."""
    if tool_id not in pending_tools:
        return

    # Content can be string or list of content blocks
    if isinstance(content, list):
        text_parts = []
        for block in content:
            if isinstance(block, dict) and block.get("type") == "text":
                text_parts.append(block.get("text", ""))
            elif isinstance(block, str):
                text_parts.append(block)
        content = "\n".join(text_parts)

    pending_tools[tool_id]["result"] = content

    tool_name = pending_tools[tool_id]["tool"]

    # Track knowledge docs from results
    if tool_name == "zerops_knowledge":
        doc_match = re.search(r"topResult:\s*(\S+)", str(content))
        if doc_match:
            knowledge_docs_used.append(doc_match.group(1))
        for m in re.findall(r"knowledge/(\S+\.md)", str(content)):
            if m not in knowledge_docs_used:
                knowledge_docs_used.append(m)

    # Track errors from results
    result_str = str(content).lower()
    error_patterns = ["error", "failed", "invalid", "not found",
                      "not_found", "auth_required", "bad_request"]
    if any(err in result_str for err in error_patterns):
        errors.append({
            "tool": tool_name,
            "message": str(content)[:300],
        })

    tool_calls.append(pending_tools.pop(tool_id))


def detect_retries(tool_calls):
    """Detect tool calls that look like retries (same tool, similar input)."""
    retries = []
    seen = {}
    for i, tc in enumerate(tool_calls):
        key = tc["tool"]
        # For import/env calls, include service hostname in key
        inp = tc.get("input", {})
        if isinstance(inp, dict):
            svc = inp.get("serviceHostname", "")
            if svc:
                key += f":{svc}"

        if key in seen:
            retries.append({
                "tool": tc["tool"],
                "first_index": seen[key],
                "retry_index": i,
            })
        else:
            seen[key] = i

    return retries


def extract_tool_calls(stream_file):
    tool_calls = []
    knowledge_queries = []
    knowledge_docs_used = []
    import_yaml_generated = []
    env_vars_set = {}
    errors = []

    pending_tools = {}  # tool_use_id -> tool info

    for line in open(stream_file):
        line = line.strip()
        if not line:
            continue

        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue

        msg_type = msg.get("type")

        # Handle assistant messages with tool_use blocks
        if msg_type == "assistant":
            content = msg.get("message", {}).get("content", [])
            for block in content:
                if not isinstance(block, dict):
                    continue
                if block.get("type") != "tool_use":
                    continue

                tool_id = block.get("id", "")
                tool_name = block.get("name", "")
                tool_input = block.get("input", {})

                # Normalize tool name (remove mcp prefix variants)
                short_name = tool_name
                for prefix in ["mcp__zerops__", "mcp__zcp__", "mcp__zaia-mcp__"]:
                    short_name = short_name.replace(prefix, "")

                pending_tools[tool_id] = {
                    "tool": short_name,
                    "input": tool_input,
                    "result": None,
                }

                # Track knowledge queries
                if short_name == "zerops_knowledge":
                    query = tool_input.get("query", "")
                    if query:
                        knowledge_queries.append(query)

                # Track import YAML
                if short_name == "zerops_import":
                    content_yaml = tool_input.get("content", "")
                    if content_yaml:
                        import_yaml_generated.append(content_yaml)

                # Track env var sets
                if short_name == "zerops_env" and tool_input.get("action") == "set":
                    svc = tool_input.get("serviceHostname", "unknown")
                    variables = tool_input.get("variables", [])
                    if svc not in env_vars_set:
                        env_vars_set[svc] = []
                    env_vars_set[svc].extend(variables)

        # Handle tool results
        elif msg_type == "user":
            user_content = msg.get("message", {}).get("content", [])
            for result_block in user_content:
                if not isinstance(result_block, dict):
                    continue
                if result_block.get("type") != "tool_result":
                    continue

                tool_id = result_block.get("tool_use_id", "")
                result_content = result_block.get("content", "")

                process_tool_result(
                    tool_id, result_content, pending_tools, tool_calls,
                    knowledge_docs_used, errors
                )

    # Add any pending tools that never got results
    for tool_id, info in list(pending_tools.items()):
        info["result"] = "(no result received)"
        tool_calls.append(info)

    retries = detect_retries(tool_calls)

    return {
        "tool_calls": tool_calls,
        "knowledge_queries": knowledge_queries,
        "knowledge_docs_used": knowledge_docs_used,
        "import_yaml_generated": import_yaml_generated,
        "env_vars_set": env_vars_set,
        "errors": errors,
        "retries": retries,
        "summary": {
            "total_tool_calls": len(tool_calls),
            "total_knowledge_queries": len(knowledge_queries),
            "total_errors": len(errors),
            "total_retries": len(retries),
            "unique_knowledge_docs": len(set(knowledge_docs_used)),
        },
    }


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <stream-file.jsonl>", file=sys.stderr)
        sys.exit(1)

    result = extract_tool_calls(sys.argv[1])
    json.dump(result, sys.stdout, indent=2, ensure_ascii=False)
    print()
