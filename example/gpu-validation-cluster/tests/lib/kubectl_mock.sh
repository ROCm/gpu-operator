#!/bin/bash
# kubectl mock infrastructure for the gpu-validation-cluster bash
# test suite.
#
# Why a real executable on PATH rather than a bash function override:
# The helper library under test is sourced into the test process,
# but the orchestrator's `_run_phase_generic` writes per-phase
# scripts to /tmp and `source`s them. In CI environments where a
# real `kubectl` exists on PATH, a bash function override is only
# visible to the current shell; sub-shells that look up `kubectl`
# via execvp will find the real binary and try to talk to a real
# cluster. Putting a fake `kubectl` on PATH as the FIRST entry
# guarantees every kubectl invocation -- in any sub-shell, sourced
# script, or process substitution -- hits the mock.
#
# Usage:
# source "$(dirname "$0")/lib/kubectl_mock.sh"
# kubectl_mock_init # creates tmpdir, installs PATH
# kubectl_mock_reset # zero call log + canned state
# kubectl_mock_set_label \
# <node> <key> <value> # serve <value> for jsonpath get
# kubectl_mock_fail label 1 # next `kubectl label` exits 1
# .run SUT.
# assert_kubectl_call "label node node-a x=passed --overwrite"
# kubectl_mock_cleanup # on EXIT
#
# Call log format:
# One line per kubectl invocation, all positional args
# space-separated, e.g. `label node node-a amd.com/x=passed --overwrite`.

# --- module state ---------------------------------------------------
KUBECTL_MOCK_DIR=""
KUBECTL_CALLS_FILE=""
KUBECTL_STATE_FILE=""
KUBECTL_FAIL_DIR=""
KUBECTL_MOCK_ORIG_PATH=""

# kubectl_mock_init
# One-time setup per test process: creates a temp dir, writes a
# `kubectl` shim into it, prepends it to PATH. Registers an EXIT trap
# to clean up unless the caller already manages cleanup.
kubectl_mock_init() {
    if [[ -n "$KUBECTL_MOCK_DIR" && -d "$KUBECTL_MOCK_DIR" ]]; then
        return 0
    fi
    KUBECTL_MOCK_DIR=$(mktemp -d -t kubectl-mock-XXXXXX)
    KUBECTL_CALLS_FILE="${KUBECTL_MOCK_DIR}/calls.log"
    KUBECTL_STATE_FILE="${KUBECTL_MOCK_DIR}/state"
    KUBECTL_FAIL_DIR="${KUBECTL_MOCK_DIR}/fail"
    mkdir -p "$KUBECTL_FAIL_DIR"
    : >"$KUBECTL_CALLS_FILE"
    : >"$KUBECTL_STATE_FILE"

    KUBECTL_MOCK_ORIG_PATH="$PATH"

    # Render the shim. Note we use single-quoted heredoc so the inner
    # script sees its own variables at runtime via the env we export.
    cat >"${KUBECTL_MOCK_DIR}/kubectl" <<'KCT'
#!/bin/bash
# Mock kubectl. All invocations are recorded; verbs `label`,
# `annotate`, and `get` (with -o jsonpath=) are honored for behavioral
# tests. Any other verb returns 99 so an accidental real-world call
# in a test does not silently succeed.
#
# stdin handling: callers (especially `kubectl apply -f -`) pipe data
# into us. Drain stdin to /dev/null before exiting so the upstream
# command (e.g. `sed`) does not receive SIGPIPE. Without this, runs
# under `set -o pipefail` see intermittent rc=141 from the pipe and
# treat the apply as failed -- a classic flake.
if [[ ! -t 0 ]]; then
    cat >/dev/null 2>&1 || true
fi

CALLS="${KUBECTL_CALLS_FILE:?KUBECTL_CALLS_FILE must be set}"
STATE="${KUBECTL_STATE_FILE:?KUBECTL_STATE_FILE must be set}"
FAILDIR="${KUBECTL_FAIL_DIR:?KUBECTL_FAIL_DIR must be set}"

# Capture original argv into ARGS BEFORE any I/O so concurrent mock
# invocations cannot corrupt our view. (Phase 4's bounded-parallel
# pair_runners produce real concurrent invocations.) Previous code
# appended to CALLS then re-read it with `tail -n1`, which is racy:
# two concurrent writers can interleave printfs, and `tail -n1` may
# return another shell's last write entirely.
ARGS=( "$@" )

# Build the call-log line in memory, then append it with a SINGLE
# write so the per-line shape is preserved even if other mock procs
# are writing concurrently. POSIX guarantees that write up to
# PIPE_BUF bytes (typically 4096) to an O_APPEND fd is atomic, and
# kubectl arg lines fit easily under that limit.
call_line="$1"
shift || true
for a in "$@"; do
    call_line+=" $a"
done
printf '%s\n' "$call_line" >>"$CALLS"

verb="${ARGS[0]}"
case "$verb" in
    label|annotate)
        if [[ -f "${FAILDIR}/${verb}" ]]; then
            ec=$(cat "${FAILDIR}/${verb}")
            # one-shot: remove after consuming
            rm -f "${FAILDIR}/${verb}"
            exit "$ec"
        fi
        if [[ -f "${FAILDIR}/${verb}.sticky" ]]; then
            ec=$(cat "${FAILDIR}/${verb}.sticky")
            exit "$ec"
        fi
        exit 0
        ;;
    get)
        # Expected shape: get node <name> -o jsonpath={.}
        # We respond by looking up the requested label in state.
        # State format, one entry per line:
        # <node>|<label_key>=<value>
        if [[ -f "${FAILDIR}/get" ]]; then
            ec=$(cat "${FAILDIR}/get")
            rm -f "${FAILDIR}/get"
            exit "$ec"
        fi
        if [[ -f "${FAILDIR}/get.sticky" ]]; then
            ec=$(cat "${FAILDIR}/get.sticky")
            exit "$ec"
        fi

        # ----------------------------------------------------------
        # Phase-1-specific early route:
        # get pods -l job-name=<job> -o jsonpath={.items[-1:].metadata.name}
        # The generic `-l` arm below short-circuits with `exit 0` for
        # selector listings, so this pattern -- which combines -l with
        # a jsonpath -- must be detected first.
        # ----------------------------------------------------------
        p_jobname=""
        p_jsonpath=""
        p_is_pods=0
        # Phase-5 selector pieces. The Kubeflow MPIJob
        # selector shape is
        # training.kubeflow.org/job-name=<job>,training.kubeflow.org/replica-type=<role>
        # optionally combined with --field-selector spec.nodeName=<node>.
        # We capture each piece independently of the early-route
        # job-name= parser above so the Phase-5 routes can match without
        # disturbing existing Phase 1/4 jobname semantics.
        p_kf_jobname=""
        p_kf_replica=""
        p_field_node=""
        p_first_positional=""
        p_seen_pods=0
        pi=1
        while [[ $pi -lt ${#ARGS[@]} ]]; do
            case "${ARGS[$pi]}" in
                pods|pod)
                    p_is_pods=1
                    p_seen_pods=1
                    ;;
                -l)
                    pj=$((pi + 1))
                    if [[ $pj -lt ${#ARGS[@]} ]]; then
                        psel="${ARGS[$pj]}"
                        if [[ "$psel" == "job-name="* ]]; then
                            p_jobname="${psel#job-name=}"
                        fi
                        # Walk comma-separated selector fragments and
                        # pull out the Kubeflow keys we care about.
                        IFS=',' read -r -a _sel_parts <<<"$psel"
                        for _sp in "${_sel_parts[@]}"; do
                            case "$_sp" in
                                training.kubeflow.org/job-name=*)
                                    p_kf_jobname="${_sp#training.kubeflow.org/job-name=}"
                                    ;;
                                training.kubeflow.org/replica-type=*)
                                    p_kf_replica="${_sp#training.kubeflow.org/replica-type=}"
                                    ;;
                            esac
                        done
                    fi
                    ;;
                --field-selector)
                    pj=$((pi + 1))
                    if [[ $pj -lt ${#ARGS[@]} ]]; then
                        _fs="${ARGS[$pj]}"
                        if [[ "$_fs" == "spec.nodeName="* ]]; then
                            p_field_node="${_fs#spec.nodeName=}"
                        fi
                    fi
                    ;;
                --field-selector=*)
                    _fs="${ARGS[$pi]#--field-selector=}"
                    if [[ "$_fs" == "spec.nodeName="* ]]; then
                        p_field_node="${_fs#spec.nodeName=}"
                    fi
                    ;;
                -o)
                    pj=$((pi + 1))
                    if [[ $pj -lt ${#ARGS[@]} ]]; then
                        p_jsonpath="${ARGS[$pj]}"
                    fi
                    ;;
                *)
                    # First non-flag positional after the verb. Used by
                    # the Phase-5 exit-code route which targets a single
                    # pod by name: `kubectl get pod <pod_name> -o .`.
                    if [[ "$p_seen_pods" -eq 1 && -z "$p_first_positional" \
                            && "${ARGS[$pi]}" != -* ]]; then
                        p_first_positional="${ARGS[$pi]}"
                    fi
                    ;;
            esac
            pi=$((pi + 1))
        done

        # ----------------------------------------------------------
        # Phase-5 per-worker pod lookups.
        # PHASE5_SCRIPT issues:
        # a) get pods -l training.kubeflow.org/job-name=<job>,
        # training.kubeflow.org/replica-type=worker
        # --field-selector spec.nodeName=<node>
        # -o jsonpath='{.items[0].metadata.name}'
        # -> worker pod name on that node (or empty)
        # b) get pods -l training.kubeflow.org/job-name=<job>,
        # training.kubeflow.org/replica-type=launcher
        # -o jsonpath='{.items[0].metadata.name}'
        # -> launcher pod name (or empty)
        # c) get pod <pod_name> -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}'
        # -> per-worker container exit code (or empty)
        # ----------------------------------------------------------
        if [[ "$p_is_pods" -eq 1 && -n "$p_kf_jobname" \
                && "$p_kf_replica" == "worker" && -n "$p_field_node" \
                && "$p_jsonpath" == *".items"*".metadata.name"* ]]; then
            val=""
            while IFS= read -r line; do
                if [[ "$line" == "phase5-worker-pod|${p_kf_jobname}|${p_field_node}="* ]]; then
                    val="${line#phase5-worker-pod|${p_kf_jobname}|${p_field_node}=}"
                fi
            done <"$STATE"
            printf '%s' "$val"
            exit 0
        fi
        if [[ "$p_is_pods" -eq 1 && -n "$p_kf_jobname" \
                && "$p_kf_replica" == "launcher" \
                && "$p_jsonpath" == *".items"*".metadata.name"* ]]; then
            val=""
            while IFS= read -r line; do
                if [[ "$line" == "phase5-launcher-pod|${p_kf_jobname}="* ]]; then
                    val="${line#phase5-launcher-pod|${p_kf_jobname}=}"
                fi
            done <"$STATE"
            printf '%s' "$val"
            exit 0
        fi
        if [[ "$p_is_pods" -eq 1 && -n "$p_first_positional" \
                && "$p_jsonpath" == *"terminated.exitCode"* ]]; then
            val=""
            while IFS= read -r line; do
                if [[ "$line" == "phase5-pod-exit|${p_first_positional}="* ]]; then
                    val="${line#phase5-pod-exit|${p_first_positional}=}"
                fi
            done <"$STATE"
            printf '%s' "$val"
            exit 0
        fi
        if [[ "$p_is_pods" -eq 1 && -n "$p_jobname" \
                && "$p_jsonpath" == *".items"*".metadata.name"* ]]; then
            val=""
            while IFS= read -r line; do
                if [[ "$line" == "pod-for-job|${p_jobname}="* ]]; then
                    val="${line#pod-for-job|${p_jobname}=}"
                fi
            done <"$STATE"
            printf '%s' "$val"
            exit 0
        fi

        # ----------------------------------------------------------
        # Phase-4-specific routes:
        # get pods -l job-name=<job> -o jsonpath={.items[0].status.podIP}
        # -- look up pod-ip|<job>=<ip> from state.
        # get pods -l job-name=<job> --no-headers (no -o)
        # -- list one pod-name line per seeded pod-for-job entry;
        # PHASE4_DRIVER_SCRIPT only uses `wc -l` on the result
        # to count whether ANY pod exists (admission check).
        # Both patterns are pod selectors with -l job-name=., so they
        # must be detected before the generic -l arm below short-circuits.
        # ----------------------------------------------------------
        if [[ "$p_is_pods" -eq 1 && -n "$p_jobname" \
                && "$p_jsonpath" == *".items"*".status.podIP"* ]]; then
            val=""
            while IFS= read -r line; do
                if [[ "$line" == "pod-ip|${p_jobname}="* ]]; then
                    val="${line#pod-ip|${p_jobname}=}"
                fi
            done <"$STATE"
            printf '%s' "$val"
            exit 0
        fi
        if [[ "$p_is_pods" -eq 1 && -n "$p_jobname" \
                && -z "$p_jsonpath" ]]; then
            # No -o flag: emit one line per seeded pod-for-job entry
            # (matches `kubectl get pods -l job-name=X --no-headers` shape
            # closely enough for `wc -l`-based existence checks).
            while IFS= read -r line; do
                if [[ "$line" == "pod-for-job|${p_jobname}="* ]]; then
                    pod_name="${line#pod-for-job|${p_jobname}=}"
                    printf '%s\n' "$pod_name"
                fi
            done <"$STATE"
            exit 0
        fi

        # ----------------------------------------------------------
        # Phase-4.5 worker-pod listing routes.
        # PHASE45_PREFLIGHT_SCRIPT issues three pod listings:
        # 1. get pods -n NS -l <labels> -o jsonpath={.items[*].metadata.name}
        # -> space-separated pod names (KUBECTL_MOCK_POD_NAMES)
        # 2. get pods -n NS -l <labels> -o jsonpath={.items[*].status.podIP}
        # -> space-separated pod IPs (KUBECTL_MOCK_POD_IPS)
        # 3. get pods -n NS -l <labels>
        # -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}'
        # -> one node name per line (KUBECTL_MOCK_NODE_NAMES,
        # \n-separated input)
        # Plus the MPIJob discovery:
        # 4. get mpijob -o jsonpath={.items[*].metadata.name}
        # -> space-separated mpijob names (KUBECTL_MOCK_MPIJOB_NAMES)
        # The pre-flight script does NOT use the kubeflow selector by
        # `job-name=`, so it bypasses the earlier early-routes above.
        # We key by the jsonpath shape rather than the selector content
        # so tests don't have to mirror the exact label string.
        # ----------------------------------------------------------
        p_is_mpijob=0
        p_mpijob_name=""
        for ai_idx in "${!ARGS[@]}"; do
            ai="${ARGS[$ai_idx]}"
            case "$ai" in
                mpijob|mpijobs)
                    p_is_mpijob=1
                    # The next non-flag positional after `mpijob` is the
                    # job name when present (e.g. `get mpijob <name>`).
                    ai_next=$((ai_idx + 1))
                    if [[ $ai_next -lt ${#ARGS[@]} ]]; then
                        nxt="${ARGS[$ai_next]}"
                        if [[ "$nxt" != -* ]]; then
                            p_mpijob_name="$nxt"
                        fi
                    fi
                    ;;
            esac
        done
        if [[ "$p_is_mpijob" -eq 1 \
                && "$p_jsonpath" == *".items"*".metadata.name"* ]]; then
            while IFS= read -r line; do
                if [[ "$line" == "phase45-mpijob-names="* ]]; then
                    printf '%s' "${line#phase45-mpijob-names=}"
                fi
            done <"$STATE"
            exit 0
        fi

        # ----------------------------------------------------------
        # Phase-5 MPIJob terminal-condition polling.
        # PHASE5_SCRIPT polls both Succeeded and Failed conditions
        # every 5s instead of `kubectl wait --for=condition=Succeeded`,
        # so a Failed MPIJob short-circuits within ~5s instead of
        # blocking for MPIJOB_WAIT_TIME.
        # Shape:
        # get mpijob <name> -o jsonpath={.status.conditions[?(@.type=="Succeeded")].status}
        # get mpijob <name> -o jsonpath={.status.conditions[?(@.type=="Failed")].status}
        # Seed via: kubectl_mock_set_mpijob_condition <name> <type> <status>
        # ----------------------------------------------------------
        if [[ "$p_is_mpijob" -eq 1 && -n "$p_mpijob_name" \
                && -n "$p_jsonpath" ]]; then
            mp_cond=""
            case "$p_jsonpath" in
                *'@.type=="Succeeded"'*) mp_cond="Succeeded" ;;
                *'@.type=="Failed"'*)    mp_cond="Failed" ;;
            esac
            if [[ -n "$mp_cond" ]]; then
                val=""
                while IFS= read -r line; do
                    if [[ "$line" == "mpijob|${p_mpijob_name}=${mp_cond}="* ]]; then
                        val="${line#mpijob|${p_mpijob_name}=${mp_cond}=}"
                    fi
                done <"$STATE"
                printf '%s' "$val"
                exit 0
            fi
        fi
        if [[ "$p_is_pods" -eq 1 && -z "$p_jobname" && \
                "$p_jsonpath" == *".items"*".metadata.name"* ]]; then
            while IFS= read -r line; do
                if [[ "$line" == "phase45-pod-names="* ]]; then
                    printf '%s' "${line#phase45-pod-names=}"
                fi
            done <"$STATE"
            exit 0
        fi
        if [[ "$p_is_pods" -eq 1 && -z "$p_jobname" && \
                "$p_jsonpath" == *".items"*".status.podIP"* ]]; then
            while IFS= read -r line; do
                if [[ "$line" == "phase45-pod-ips="* ]]; then
                    printf '%s' "${line#phase45-pod-ips=}"
                fi
            done <"$STATE"
            exit 0
        fi
        if [[ "$p_is_pods" -eq 1 && -z "$p_jobname" && \
                "$p_jsonpath" == *"range"*"nodeName"* ]]; then
            # node-names entry stores the lines joined by '\n' literal
            # (encoded so the one-line STATE format survives); decode
            # by replacing the literal '\n' marker with real newlines.
            while IFS= read -r line; do
                if [[ "$line" == "phase45-node-names="* ]]; then
                    enc="${line#phase45-node-names=}"
                    printf '%s' "$enc" | sed 's|<NL>|\
|g'
                fi
            done <"$STATE"
            exit 0
        fi

        # Walk args to find `node <name>` and `-o jsonpath=.`.
        node=""
        jp=""
        i=1
        while [[ $i -lt ${#ARGS[@]} ]]; do
            case "${ARGS[$i]}" in
                node|nodes)
                    j=$((i + 1))
                    if [[ $j -lt ${#ARGS[@]} ]]; then
                        node="${ARGS[$j]}"
                    fi
                    ;;
                -o)
                    j=$((i + 1))
                    if [[ $j -lt ${#ARGS[@]} ]]; then
                        jp="${ARGS[$j]}"
                    fi
                    ;;
                -l)
                    # selector-based list; emit names of state-tracked nodes
                    # that have the matching label=value (very small subset
                    # of real selector semantics, but enough for DRY_RUN
                    # tests).
                    j=$((i + 1))
                    if [[ $j -lt ${#ARGS[@]} ]]; then
                        sel="${ARGS[$j]}"
                        sel_key="${sel%%=*}"
                        sel_val="${sel#*=}"
                        # Sticky DRY_RUN harness writes selector hits in
                        # STATE using the same format. Match exact equality.
                        while IFS= read -r line; do
                            n="${line%%|*}"
                            rest="${line#*|}"
                            k="${rest%%=*}"
                            v="${rest#*=}"
                            if [[ "$k" == "$sel_key" && "$v" == "$sel_val" ]]; then
                                printf 'node/%s\n' "$n"
                            fi
                        done <"$STATE"
                    fi
                    exit 0
                    ;;
            esac
            i=$((i + 1))
        done

        # jsonpath of the form {.metadata.labels.<escaped-key>}
        if [[ -n "$jp" ]]; then
            inner="${jp#jsonpath=}"
            inner="${inner#\{}"
            inner="${inner%\}}"

            # ----------------------------------------------------------
            # Phase-1-specific jsonpath shapes (added for):
            # * {.status.conditions[?(@.type=="Complete")].status}
            # -> look up state line: job|<jobname>=Complete=<val>
            # * {.status.conditions[?(@.type=="Failed")].status}
            # -> look up state line: job|<jobname>=Failed=<val>
            # * {.items[-1:].metadata.name} (with -l job-name=X)
            # -> look up state line: pod-for-job|<jobname>=<podname>
            # ----------------------------------------------------------
            cond_type=""
            case "$inner" in
                *'@.type=="Complete"'*) cond_type="Complete" ;;
                *'@.type=="Failed"'*)   cond_type="Failed" ;;
            esac
            if [[ -n "$cond_type" ]]; then
                # We need the job name from ARGS. Re-walk to find it.
                jobname=""
                ii=1
                while [[ $ii -lt ${#ARGS[@]} ]]; do
                    case "${ARGS[$ii]}" in
                        job|jobs)
                            jj=$((ii + 1))
                            if [[ $jj -lt ${#ARGS[@]} ]]; then
                                jobname="${ARGS[$jj]}"
                            fi
                            ;;
                    esac
                    ii=$((ii + 1))
                done
                if [[ -n "$jobname" ]]; then
                    val=""
                    while IFS= read -r line; do
                        if [[ "$line" == "job|${jobname}=${cond_type}="* ]]; then
                            val="${line#job|${jobname}=${cond_type}=}"
                        fi
                    done <"$STATE"
                    printf '%s' "$val"
                fi
                exit 0
            fi

            # Note: pod-by-job-name lookup is handled by the early
            # route at the top of the `get` arm (above), because the
            # generic `-l` selector path exits before we reach here.

            # Default: treat as a node-label lookup
            # ({.metadata.labels.<escaped-key>}).
            inner="${inner#.metadata.labels.}"
            # Unescape backslash-dots back to dots.
            key="${inner//\\./.}"
            val=""
            while IFS= read -r line; do
                n="${line%%|*}"
                rest="${line#*|}"
                if [[ "$n" == "$node" && "$rest" == "${key}="* ]]; then
                    val="${rest#${key}=}"
                fi
            done <"$STATE"
            printf '%s' "$val"
        fi
        exit 0
        ;;
    delete|apply|patch|create)
        # Allowed but no-op for the DRY_RUN orchestrator tests.
        # Fail-injection support added for (PHASE1_SCRIPT
        # job-creation-failure path needs to drive `apply` non-zero).
        # Same one-shot vs. sticky semantics as label|annotate above.
        if [[ -f "${FAILDIR}/${verb}" ]]; then
            ec=$(cat "${FAILDIR}/${verb}")
            rm -f "${FAILDIR}/${verb}"
            exit "$ec"
        fi
        if [[ -f "${FAILDIR}/${verb}.sticky" ]]; then
            ec=$(cat "${FAILDIR}/${verb}.sticky")
            exit "$ec"
        fi
        exit 0
        ;;
    wait)
        # `kubectl wait` for PHASE45_PREFLIGHT_SCRIPT.
        # Honors the same one-shot vs sticky failure-injection knobs
        # as label/annotate above. Default is success so the script
        # proceeds to the SSH-mesh / DNS / MPI / RCCL checks under test.
        if [[ -f "${FAILDIR}/wait" ]]; then
            ec=$(cat "${FAILDIR}/wait")
            rm -f "${FAILDIR}/wait"
            exit "$ec"
        fi
        if [[ -f "${FAILDIR}/wait.sticky" ]]; then
            ec=$(cat "${FAILDIR}/wait.sticky")
            exit "$ec"
        fi
        exit 0
        ;;
    exec)
        # `kubectl exec [-n NS] POD -- CMD .` for PHASE45_PREFLIGHT_SCRIPT
        #. The pre-flight script issues many exec calls
        # back-to-back (launcher->worker readiness probe, N*N SSH mesh,
        # DNS, MPI spawn, RCCL topology). Tests need to control the
        # exit code AND stdout body of each one independently.
        #
        # Mechanism: an in-order response queue. Each entry seeds one
        # exec response; entries are consumed FIFO. When the queue is
        # empty, exec returns 0 with empty stdout (the harmless
        # "everything is fine" default).
        #
        # Queue file: ${KUBECTL_FAIL_DIR}/exec-queue
        # one line per pending response:
        # <exit_code>|<base64-of-stdout>
        # Counter file: ${KUBECTL_FAIL_DIR}/exec-cursor
        # integer, next line index to consume (1-based)
        #
        # Note: we co-locate the queue under KUBECTL_FAIL_DIR rather
        # than KUBECTL_MOCK_DIR because the latter is intentionally
        # NOT exported (only the file paths the shim needs are), and
        # the exec arm needs the queue path visible to sub-shells
        # spawned by the SUT.
        queue_file="${KUBECTL_FAIL_DIR}/exec-queue"
        cursor_file="${KUBECTL_FAIL_DIR}/exec-cursor"
        ec=0
        body_b64=""
        if [[ -f "$queue_file" ]]; then
            idx=1
            if [[ -f "$cursor_file" ]]; then
                idx=$(cat "$cursor_file")
            fi
            line=$(sed -n "${idx}p" "$queue_file")
            if [[ -n "$line" ]]; then
                ec="${line%%|*}"
                body_b64="${line#*|}"
                echo $((idx + 1)) >"$cursor_file"
            fi
        fi
        if [[ -n "$body_b64" ]]; then
            # The decoded body is written verbatim. If it does not
            # already end in a newline, append one -- the SUT's DNS
            # check uses `while read -r` over the captured output and
            # would otherwise drop a final unterminated line.
            decoded=$(printf '%s' "$body_b64" | base64 -d 2>/dev/null || echo "")
            if [[ -n "$decoded" ]]; then
                printf '%s' "$decoded"
                # Add trailing newline only if missing.
                last_char="${decoded: -1}"
                if [[ "$last_char" != $'\n' ]]; then
                    printf '\n'
                fi
            fi
        fi
        exit "$ec"
        ;;
    logs)
        # `kubectl logs <pod> [--tail=N]` for PHASE2_SCRIPT.
        # The script greps a single pod's container log for marker lines
        # to classify failure reasons. We serve canned log content from
        # STATE keyed by pod name; the --tail=N flag is honored as a
        # tail-count, defaulting to "all" when absent.
        if [[ -f "${FAILDIR}/logs" ]]; then
            ec=$(cat "${FAILDIR}/logs")
            rm -f "${FAILDIR}/logs"
            exit "$ec"
        fi
        if [[ -f "${FAILDIR}/logs.sticky" ]]; then
            ec=$(cat "${FAILDIR}/logs.sticky")
            exit "$ec"
        fi
        pod_name=""
        tail_n=""
        li=1
        while [[ $li -lt ${#ARGS[@]} ]]; do
            case "${ARGS[$li]}" in
                --tail=*) tail_n="${ARGS[$li]#--tail=}" ;;
                -*)       : ;;
                *)
                    # First non-flag positional after `logs` is the pod
                    # name. PHASE2_SCRIPT never passes a -n namespace, so
                    # this is unambiguous.
                    if [[ -z "$pod_name" ]]; then
                        pod_name="${ARGS[$li]}"
                    fi
                    ;;
            esac
            li=$((li + 1))
        done
        # State lookup: line shape is
        # pod-log|<pod>=<base64-of-log>
        # base64 keeps newlines and quotes intact in the single-line
        # state file. When the seed helper is not used, we serve an
        # empty body (the SUT treats absent logs as "no markers found"
        # and falls back to the default reason).
        encoded=""
        while IFS= read -r line; do
            if [[ "$line" == "pod-log|${pod_name}="* ]]; then
                encoded="${line#pod-log|${pod_name}=}"
            fi
        done <"$STATE"
        if [[ -n "$encoded" ]]; then
            decoded=$(printf '%s' "$encoded" | base64 -d 2>/dev/null || echo "")
            if [[ -n "$tail_n" ]]; then
                printf '%s' "$decoded" | tail -n "$tail_n"
            else
                printf '%s' "$decoded"
            fi
        fi
        exit 0
        ;;
esac
exit 99
KCT
    chmod +x "${KUBECTL_MOCK_DIR}/kubectl"

    export KUBECTL_CALLS_FILE KUBECTL_STATE_FILE KUBECTL_FAIL_DIR
    export PATH="${KUBECTL_MOCK_DIR}:${PATH}"
    trap kubectl_mock_cleanup EXIT
}

# kubectl_mock_cleanup
# Tear down the mock. Safe to call multiple times; idempotent.
kubectl_mock_cleanup() {
    if [[ -n "$KUBECTL_MOCK_DIR" && -d "$KUBECTL_MOCK_DIR" ]]; then
        rm -rf "$KUBECTL_MOCK_DIR"
    fi
    if [[ -n "$KUBECTL_MOCK_ORIG_PATH" ]]; then
        export PATH="$KUBECTL_MOCK_ORIG_PATH"
    fi
    KUBECTL_MOCK_DIR=""
    KUBECTL_CALLS_FILE=""
    KUBECTL_STATE_FILE=""
    KUBECTL_FAIL_DIR=""
    KUBECTL_MOCK_ORIG_PATH=""
}

# kubectl_mock_reset
# Zero out call log, label state, and any pending fail injections.
# Use between tests to keep them independent.
kubectl_mock_reset() {
    : >"$KUBECTL_CALLS_FILE"
    : >"$KUBECTL_STATE_FILE"
    # `rm -f $KUBECTL_FAIL_DIR/*` also clears the Phase 4.5 exec queue
    # / exec cursor, which live under KUBECTL_FAIL_DIR
    # for export visibility -- see the `exec` arm of the kubectl shim.
    rm -f "$KUBECTL_FAIL_DIR"/*
}

# kubectl_mock_set_label <node> <key> <value>
# Seed the canned label state served by `kubectl get node . jsonpath`.
kubectl_mock_set_label() {
    local node="$1"
    local key="$2"
    local val="$3"
    printf '%s|%s=%s\n' "$node" "$key" "$val" >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_fail <verb> <exit_code>
# Cause the next `kubectl <verb>` to exit with <exit_code>. One-shot
# (cleared after the first match). Verb is one of: label, annotate, get.
kubectl_mock_fail() {
    local verb="$1"
    local ec="$2"
    echo "$ec" >"${KUBECTL_FAIL_DIR}/${verb}"
}

# kubectl_mock_fail_sticky <verb> <exit_code>
# Same as kubectl_mock_fail but persistent until the next reset.
kubectl_mock_fail_sticky() {
    local verb="$1"
    local ec="$2"
    echo "$ec" >"${KUBECTL_FAIL_DIR}/${verb}.sticky"
}

# kubectl_mock_set_job_condition <job_name> <cond_type> <status>
# Seed the canned response for
# kubectl get job <job_name> -o jsonpath='{.status.conditions[?(@.type=="<cond_type>")].status}'
# cond_type is "Complete" or "Failed"; status is typically "True" or "".
# Used by PHASE1_SCRIPT tests.
kubectl_mock_set_job_condition() {
    local job_name="$1"
    local cond_type="$2"
    local status="$3"
    printf 'job|%s=%s=%s\n' "$job_name" "$cond_type" "$status" \
        >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_set_pod_for_job <job_name> <pod_name>
# Seed the canned response for
# kubectl get pods -l job-name=<job_name> -o jsonpath='{.items[-1:].metadata.name}'
# Used by PHASE1_SCRIPT tests.
kubectl_mock_set_pod_for_job() {
    local job_name="$1"
    local pod_name="$2"
    printf 'pod-for-job|%s=%s\n' "$job_name" "$pod_name" \
        >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_set_pod_ip_for_job <job_name> <pod_ip>
# Seed the canned response for
# kubectl get pods -l job-name=<job_name> -o jsonpath='{.items[0].status.podIP}'
# Used by PHASE4_DRIVER_SCRIPT tests -- the driver polls
# for the server pod's IP before submitting the client Job. Empty
# string means "no pod IP yet"; pair with kubectl_mock_set_pod_for_job
# absent to simulate "no pod ever created" (admission rejected).
kubectl_mock_set_pod_ip_for_job() {
    local job_name="$1"
    local pod_ip="$2"
    printf 'pod-ip|%s=%s\n' "$job_name" "$pod_ip" \
        >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_set_pod_log <pod_name> <log_file_or_text>
# Seed the canned response for `kubectl logs <pod_name> [--tail=N]`.
# The second arg may be either a path to a file (preferred for fixtures)
# or a literal string. The body is base64-encoded into state to survive
# the one-line-per-entry format. Used by PHASE2_SCRIPT tests.
kubectl_mock_set_pod_log() {
    local pod_name="$1"
    local src="$2"
    local body=""
    if [[ -f "$src" ]]; then
        body=$(cat "$src")
    else
        body="$src"
    fi
    local encoded
    encoded=$(printf '%s' "$body" | base64 | tr -d '\n')
    printf 'pod-log|%s=%s\n' "$pod_name" "$encoded" \
        >>"$KUBECTL_STATE_FILE"
}

# --- Phase 4.5 seed helpers ---------------------------
# PHASE45_PREFLIGHT_SCRIPT discovers worker pods via three pod
# listings (names, IPs, nodeNames) plus an MPIJob discovery. Tests
# seed those answers verbatim instead of teaching the mock the
# kubeflow selector grammar.

# kubectl_mock_set_mpijob_names <space-separated names>
# Drives `kubectl get mpijob -o jsonpath='{.items[*].metadata.name}'`.
kubectl_mock_set_mpijob_names() {
    printf 'phase45-mpijob-names=%s\n' "$1" >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_set_pod_names <space-separated names>
# Drives `kubectl get pods -n NS -l . -o jsonpath='{.items[*].metadata.name}'`
# when there is no `job-name=` selector (Phase 4.5 uses Kubeflow
# training labels, not job-name=).
kubectl_mock_set_pod_names() {
    printf 'phase45-pod-names=%s\n' "$1" >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_set_pod_ips <space-separated IPs>
# Drives `kubectl get pods -n NS -l . -o jsonpath='{.items[*].status.podIP}'`
# when there is no `job-name=` selector.
kubectl_mock_set_pod_ips() {
    printf 'phase45-pod-ips=%s\n' "$1" >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_set_node_names <name1> [<name2> .]
# Drives `kubectl get pods -n NS -l . -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}'`.
# Stored with a literal `<NL>` separator so the one-line-per-entry
# STATE format survives; the mock decodes back to real newlines.
kubectl_mock_set_node_names() {
    local first=1
    local encoded=""
    local n
    for n in "$@"; do
        if [[ "$first" -eq 1 ]]; then
            encoded="$n"
            first=0
        else
            encoded="${encoded}<NL>${n}"
        fi
    done
    # Trailing newline mirrors the real jsonpath output shape.
    encoded="${encoded}<NL>"
    printf 'phase45-node-names=%s\n' "$encoded" >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_queue_exec <exit_code> [stdout]
# Enqueue the next response for `kubectl exec .`. Responses are
# consumed FIFO; when the queue is empty the mock falls back to exit 0
# with empty stdout. The optional stdout body is base64-encoded into
# the queue so embedded newlines and quotes survive the one-line shape.
kubectl_mock_queue_exec() {
    local ec="$1"
    local body="${2:-}"
    local encoded=""
    if [[ -n "$body" ]]; then
        encoded=$(printf '%s' "$body" | base64 | tr -d '\n')
    fi
    printf '%s|%s\n' "$ec" "$encoded" \
        >>"${KUBECTL_FAIL_DIR}/exec-queue"
}

# --- Phase 5 seed helpers -----------------------------
# PHASE5_SCRIPT looks up per-worker pods by (job_name, node_name) and
# reads per-pod terminated.exitCode for failure-attribution. Tests
# seed these answers verbatim instead of teaching the mock the full
# Kubeflow selector grammar.

# kubectl_mock_set_phase5_worker_pod_for_node <job_name> <node> <pod_name>
# Drives the Phase-5 per-node worker-pod lookup:
# get pods -l training.kubeflow.org/job-name=<job>,
# training.kubeflow.org/replica-type=worker
# --field-selector spec.nodeName=<node>
# -o jsonpath='{.items[0].metadata.name}'
# An empty <pod_name> simulates "no worker pod scheduled on that node"
# (the SUT swallows the empty result via `|| true`).
kubectl_mock_set_phase5_worker_pod_for_node() {
    local job_name="$1"
    local node="$2"
    local pod_name="$3"
    printf 'phase5-worker-pod|%s|%s=%s\n' \
        "$job_name" "$node" "$pod_name" >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_set_phase5_launcher_pod <job_name> <pod_name>
# Drives the Phase-5 launcher-pod lookup:
# get pods -l training.kubeflow.org/job-name=<job>,
# training.kubeflow.org/replica-type=launcher
# -o jsonpath='{.items[0].metadata.name}'
# Used by the launcher-log collection step.
kubectl_mock_set_phase5_launcher_pod() {
    local job_name="$1"
    local pod_name="$2"
    printf 'phase5-launcher-pod|%s=%s\n' \
        "$job_name" "$pod_name" >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_set_mpijob_condition <job_name> <cond_type> <status>
# Seed the canned response for
# kubectl get mpijob <job_name> -o jsonpath='{.status.conditions[?(@.type=="<cond_type>")].status}'
# cond_type is "Succeeded" or "Failed"; status is typically "True" or "".
# Used by the new PHASE5_SCRIPT wait-loop (polls both conditions; exits
# as soon as either reaches True instead of waiting for the full
# MPIJOB_WAIT_TIME budget).
kubectl_mock_set_mpijob_condition() {
    local job_name="$1"
    local cond_type="$2"
    local status="$3"
    printf 'mpijob|%s=%s=%s\n' "$job_name" "$cond_type" "$status" \
        >>"$KUBECTL_STATE_FILE"
}

# kubectl_mock_set_phase5_pod_exit_code <pod_name> <exit_code>
# Drives the Phase-5 per-pod exit-code lookup:
# get pod <pod_name> -o jsonpath='{.status.containerStatuses[0].state.terminated.exitCode}'
# An unset entry causes the mock to emit empty stdout, which the SUT
# coerces to "unknown" -- matches the production fallback.
kubectl_mock_set_phase5_pod_exit_code() {
    local pod_name="$1"
    local exit_code="$2"
    printf 'phase5-pod-exit|%s=%s\n' \
        "$pod_name" "$exit_code" >>"$KUBECTL_STATE_FILE"
}
