# 리모트 저장소 운영

GitHub 저장소 `sensibile/thesmothetai`의 설정과 PR 절차를 기록한다. 실제 강제는 아래 GitHub 설정이 담당하며, 이 문서가 권한을 부여하거나 제한하지 않는다. 설정은 `sensibile/taeguk`의 [리모트 저장소 운영](https://github.com/sensibile/taeguk/blob/main/docs/repository-operations.md)과 같게 맞췄다.

## PR 플로우

1. 작업 브랜치에서 변경하고 `go vet ./...`, `go test ./...`를 실행한다. gate 스크립트를 바꾸면 `python3 -m unittest discover -s scripts/tests`도 실행한다.
2. PR을 만든다. `main`에는 직접 push할 수 없다. PR 제목과 본문에는 `@codex` 문자열을 쓰지 않는다.
3. Codex가 PR 생성 리뷰를 스스로 시작한다. PR을 맡은 세션은 2분 동안 그 head에 대한 리뷰 요약 댓글(`codex-review-state`)이 붙는지 확인하고, 붙지 않을 때만 BOSS 계정(세션의 `gh` 인증 계정)으로 `@codex review` 댓글을 남긴다. PR 제목·본문·댓글에 `@codex` 문자열이 있으면, 백틱 안이라도 멘션으로 읽혀 리뷰 대신 Codex 클라우드 작업이 호출된다. 이때는 gate가 판정할 요약과 결과 표식(`codex-review-receipt`)이 남지 않는다(태극 PR #13~#16). 그래서 리뷰를 요청하는 댓글 말고는 이 문자열을 쓰지 않는다. 가리킬 때는 "Codex 리뷰 요청"처럼 풀어 쓴다.
4. `Codex review gate` workflow는 PR head 커밋에 대한 Codex 리뷰 결과를 commit status `codex-review`로 남기는 판정만 한다. 리뷰를 요청하거나 머지하지 않는다. 근거는 `chatgpt-codex-connector[bot]` 요약 댓글의 `codex-review-state` 상태다. 판정은 다음과 같다.
   - head 커밋에 대한 리뷰가 `completed`이고 미해소 지적이 없으면(`has_unresolved_findings: false`) success다. 미해소 지적이 있으면 failure, 그 밖에는 pending이다.
   - `has_unresolved_findings: null`이면, 같은 head 커밋에 대한 Codex의 최신 결과 댓글이 "Didn't find any major issues"이고 Codex가 시작한 미해결 리뷰 thread가 0개일 때만 success다.
   - 시간이 지나도 판정은 바뀌지 않는다. Codex가 그 head를 리뷰하지 않으면 pending에 머문다.
   - 댓글이나 status 조회가 실패해 근거를 읽지 못하면 pending을 게시한 뒤 실패로 끝난다. 같은 head의 이전 success가 바뀐 리뷰 대신 머지 조건을 채우지 않게 하기 위해서다.
   - Codex 요약 댓글이 삭제되거나 PR base가 바뀌면 gate가 이전 리뷰를 무효로 표시하는 댓글(head 표식 포함)을 남긴다. 무효 표시 이후 Codex가 요약을 갱신해야 다시 판정한다. 무효 표시 댓글을 남기지 못하면 `record` job이 pending을 게시한 뒤 실패로 끝난다.
5. 리뷰 요청은 PR을 맡은 세션이 BOSS 계정으로 한다. Codex는 `github-actions[bot]`의 요청에는 응답하지 않았고(태극 PR #3, archon PR #12), 사람 계정 요청에는 13초~2분 안에 응답했다. 새 커밋을 push하면 Codex가 "New commits" 트리거로 다시 리뷰한다. 세션은 다음 경우에 그 head에 `@codex review`를 한 번 남긴다: PR을 열고 2분 안에 리뷰 요약이 붙지 않았을 때(3단계), 새 커밋을 push했을 때, gate가 무효 표시 댓글을 남겼을 때. 그 head에 대한 Codex 결과 댓글이 이미 있거나 리뷰가 `running`이면 남기지 않는다. 요청 뒤 10분 넘게 Codex가 시작하지 않거나 `running`에서 멈추면 한 번 더 요청하고, 그래도 응답이 없으면 BOSS에게 보고한다. 멈춤에는 Auto-fix 이벤트가 오지 않으므로 세션이 직접 다시 확인한다. 응답이 없다고 리뷰 없이 머지하지 않는다.
6. 머지도 PR을 맡은 세션이 한다. PR head의 `codex-review`가 success이고 미해결 리뷰 thread가 없으면 `gh pr merge <번호> --squash --auto --match-head-commit <리뷰한 head SHA>`로 auto-merge를 켠다. 머지는 리뷰한 커밋으로 고정되고, 나머지 필수 체크가 통과하면 GitHub가 머지한다. draft PR이나 `no-auto-merge` label이 붙은 PR에는 켜지 않는다. 세션이 없는 PR은 `codex-review`가 success여도 사람이 머지할 때까지 대기한다.

CodeQL default setup이 PR을 분석하지 않아 머지가 막히면 빈 커밋을 push해 분석을 다시 실행한다. 저장소 첫 설정 직후 연 PR에서 한 번 발생했다.

gate는 태극(sensibile/taeguk) main `f761a32`의 `scripts/codex_review_gate.py`, `tests/test_codex_review_gate.py`(여기서는 `scripts/tests/`), `.github/workflows/codex-review-gate.yml`을 그대로 가져왔다. gate workflow는 `pull_request_target`과 `issue_comment`로만 실행된다. 두 이벤트 모두 기본 브랜치의 workflow와 스크립트를 실행하므로 PR이 자기 판정 코드를 바꿀 수 없다. 그래서 gate를 처음 넣는 PR에서는 gate가 돌지 않는다. 요약 댓글의 상태 표식은 Codex의 공개 API가 아니다. 형식이 바뀌어 읽을 수 없으면 gate는 pending에 머물고, 사람이 Codex 결과를 확인해 처리한다. gate는 저장소 writer를 신뢰하는 전제의 자동화이며 writer에 대한 보안 경계는 아니다. fork PR과 기본 브랜치가 아닌 base로 향하는 PR은 처리하지 않는다. 스크립트 단위 테스트는 `.github/workflows/gate-tests.yml`이 돌린다.

## GitHub 설정

2026-10-23에 적용하고 API 조회로 반영을 확인했다.

- ruleset `main protection`(id 25699905): 기본 브랜치에 적용한다. PR 필수, 필요 승인 0, 리뷰 스레드 해소 필수, squash 머지만 허용, CodeQL error 또는 high 이상 보안 경고 시 머지 차단, 브랜치 삭제와 force push 금지, bypass 대상 없음.
- 머지 방식: squash만 허용한다. 커밋 제목은 PR 제목, 본문은 PR 본문을 쓴다. 머지 후 브랜치를 자동 삭제한다. auto-merge를 허용한다.
- 보안: Dependabot alerts와 security updates, CodeQL default setup(go), secret scanning과 push protection.
- Codex 리뷰: ChatGPT Codex Connector 앱의 저장소 접근과 Codex cloud 설정의 Code review를 켰다. 이 설정은 GitHub가 아니라 ChatGPT 계정에서 관리한다.
- 2026-10-26 추가: ruleset 필수 체크에 commit status `codex-review`(GitHub Actions, integration 15368)를 넣고, 머지 전에 PR 브랜치가 최신 `main`을 포함하도록 요구한다(`strict_required_status_checks_policy`). 태극과 같은 구성이다. gate가 PR #7에서 status를 게시하는 것을 확인한 뒤 적용했다. `no-auto-merge` label을 만들었다.

필요 승인을 0으로 둔 이유는 작업자와 PR 작성자가 같은 계정이고 GitHub가 자기 PR 승인을 허용하지 않기 때문이다.

## 아직 걸지 않은 규칙

Go 검사(`go vet`, `go test`)는 아직 CI workflow가 없어 필수 체크가 아니다. `gate-tests`는 gate 스크립트가 바뀔 때만 도는 workflow라 필수 체크로 걸지 않는다.
