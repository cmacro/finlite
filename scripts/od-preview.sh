#!/usr/bin/env bash
# 列出 OpenDesign 本地项目中的可预览 HTML 文件及其预览 URL。
#
# 用法:
#   scripts/od-preview.sh                 # 列出全部项目的预览 URL
#   scripts/od-preview.sh import          # 只列出项目 id 含 "import" 的
#   scripts/od-preview.sh '' --check      # 同时输出 HTTP 状态码
#
# 环境变量:
#   OD_BASE   OpenDesign 预览服务地址，默认 http://127.0.0.1:7456
#
# URL 规则: ${OD_BASE}/api/projects/<projectId>/raw/<filePath>
set -euo pipefail

BASE="${OD_BASE:-http://127.0.0.1:7456}"
FILTER="${1:-}"
CHECK=0
[ "${2:-}" = "--check" ] && CHECK=1

command -v jq >/dev/null 2>&1 || { echo "需要 jq：sudo apt install jq" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "需要 curl" >&2; exit 1; }

if ! curl -fsS --max-time 3 "$BASE/api/health" >/dev/null 2>&1; then
  echo "OpenDesign 预览服务未运行或不可达：$BASE" >&2
  exit 1
fi

projects=$(curl -fsS --max-time 5 "$BASE/api/projects" | jq -r '.projects[].id')

found=0
for id in $projects; do
  case "$id" in *"$FILTER"*) ;; *) continue ;; esac
  files=$(curl -fsS --max-time 5 "$BASE/api/projects/$id/files" \
    | jq -r '.files[] | select(.kind == "html") | .path' || true)
  if [ -z "$files" ]; then
    printf "%-30s (无可预览 HTML)\n" "$id"
    continue
  fi
  while IFS= read -r f; do
    url="$BASE/api/projects/$id/raw/$f"
    if [ "$CHECK" = 1 ]; then
      code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 4 "$url")
      printf "%s  %s\n" "$code" "$url"
    else
      echo "$url"
    fi
    found=$((found + 1))
  done <<< "$files"
done

if [ "$found" -eq 0 ]; then
  echo "没有匹配的可预览文件（filter=${FILTER:-<none>}）" >&2
fi
