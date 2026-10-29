# 리모트 저장소 운영

GitHub 저장소 `sensibile/thesmothetai`의 설정과 PR 절차를 기록한다. 실제 강제는 아래 GitHub 설정이 담당하며, 이 문서가 권한을 부여하거나 제한하지 않는다. 설정은 `sensibile/taeguk`의 [리모트 저장소 운영](https://github.com/sensibile/taeguk/blob/main/docs/repository-operations.md)과 같게 맞췄다.

## PR 플로우

2026-10-29 BOSS가 태극 #22와 같이 GitHub의 Codex 리뷰 gate(`codex-review` status)를 정리하기로 결정했다("둘 다 진행"). 대체 필수 체크는 Go CI의 `go` job으로, 게이트 단위 테스트(`Gate tests`)는 게이트와 함께 지우기로 BOSS가 이 저장소 세션에서 골랐다. 리뷰는 push 전 로컬 단계에서 하고, 머지 조건은 CI 검증이다. 이전 gate의 설계와 관측 근거는 Git 이력의 이 문서와 PR #6~#10에 남아 있다.

1. 작업 브랜치에서 변경하고 `go build ./...`, `go vet ./...`, `go test ./...`를 실행한다.
2. push 전에 PR 누적 범위(`origin/main`과의 merge-base부터 head까지)를 로컬에서 리뷰한다. 코드나 실행 동작에 영향을 주는 변경은 `taeguk_codex_cross_reviewer`와 `taeguk_codex_security_reviewer`를 `taeguk-codex-local-review` 절차로 실행한다. 문서만 바뀐 변경은 보안 리뷰를 NOT_APPLICABLE로 남긴다. 지적은 반영하거나 반영하지 않은 이유를 남긴다.
3. PR 본문에 검증 결과와 로컬 리뷰 결과(대상 SHA, 모델과 effort, 지적과 처리)를 적는다. 제목, 본문, 댓글에는 `@codex` 문자열을 쓰지 않는다. 코드 표기 안의 것도 멘션으로 읽혀 Codex 클라우드 작업을 부른다(태극 PR #13~#16).
4. 그 head SHA에 대해 2단계의 필수 로컬 리뷰가 실제로 끝났는지 확인한다. CLI 로그인 실패, timeout, 빈 출력 등으로 NOT_RUN이나 UNKNOWN이면 auto-merge를 켜지 않고 원인을 해소하거나 BOSS에게 보고한다. 확인되면 PR을 만든 세션이 `gh pr merge <번호> --squash --auto --match-head-commit <리뷰한 head SHA>`로 auto-merge를 켠다. 필수 체크가 통과하고 리뷰 thread가 모두 해소되고 PR이 최신 `main`을 포함하면 GitHub가 머지한다. draft PR이나 `no-auto-merge` label이 붙은 PR에는 켜지 않는다. 이 opt-out은 세션이 지키는 운영 규칙이며 GitHub 머지 조건이 아니다.
5. 새 커밋을 올릴 때는 먼저 `gh pr merge <번호> --disable-auto`로 auto-merge를 끈다. GitHub는 쓰기 권한자가 push해도 auto-merge를 유지하므로, 끄지 않으면 리뷰하지 않은 head가 머지될 수 있다. 그다음 push 전에 2단계의 로컬 리뷰를 새 누적 범위로 하고, 리뷰 반영으로 SHA가 바뀌면 다시 리뷰한다. 리뷰한 SHA를 push한 뒤 4단계로 다시 켠다.
6. `main`이 앞서 나가 PR이 뒤처지면 먼저 `gh pr merge <번호> --disable-auto`로 auto-merge를 끄고, `gh pr view <번호> --json autoMergeRequest`가 `null`인지 확인한 뒤 `gh pr update-branch`로 갱신한다. `--match-head-commit`은 켤 때의 head만 확인할 뿐 이후 head 변경을 막지 않기 때문이다. 갱신 뒤에는 PR 누적 diff가 같아도 2단계의 로컬 리뷰를 새 head로 다시 한 뒤 4단계로 켠다.

GitHub의 Codex 자동 리뷰는 저장소가 아니라 Codex 설정에서 켜져 있어 계속 댓글을 달 수 있다. 머지 조건은 아니지만 ruleset이 리뷰 thread 해소를 요구하므로, 세션은 thread를 판정해 반영하거나 이유를 답한 뒤 해소한다. 리뷰 도구의 결과는 검토 보조 증거이며 요구사항 충족 판정을 대신하지 않는다. 로컬 리뷰 실행 여부는 GitHub가 강제하지 않는다. 세션이 지키는 절차다.

CodeQL default setup이 PR을 분석하지 않아 머지가 막히면 빈 커밋을 push해 분석을 다시 실행한다. 저장소 첫 설정 직후 연 PR에서 한 번 발생했다.

## GitHub 설정

2026-10-23에 적용하고 API 조회로 반영을 확인했다.

- ruleset `main protection`(id 25699905): 기본 브랜치에 적용한다. PR 필수, 필요 승인 0, 리뷰 스레드 해소 필수, squash 머지만 허용, `go` 체크 통과 필수, 머지 전 최신 `main` 포함 필수(`strict_required_status_checks_policy`), CodeQL error 또는 high 이상 보안 경고 시 머지 차단, 브랜치 삭제와 force push 금지, bypass 대상 없음.
- 머지 방식: squash만 허용한다. 커밋 제목은 PR 제목, 본문은 PR 본문을 쓴다. 머지 후 브랜치를 자동 삭제한다. auto-merge를 허용한다. `no-auto-merge` label이 있다.
- 보안: Dependabot alerts와 security updates, CodeQL default setup(go), secret scanning과 push protection.
- Codex 리뷰: ChatGPT Codex Connector 앱의 저장소 접근과 Codex cloud 설정의 Code review를 켰다. 이 설정은 GitHub가 아니라 ChatGPT 계정에서 관리한다.
- 변경 이력: 2026-10-26 필수 체크 `codex-review`와 최신 `main` 포함 요구를 추가했다. 2026-10-29 `codex-review`를 빼고 `go`를 넣었다.

필요 승인을 0으로 둔 이유는 작업자와 PR 작성자가 같은 계정이고 GitHub가 자기 PR 승인을 허용하지 않기 때문이다. 필수 체크 이름 `go`는 `.github/workflows/go.yml`의 job 이름에 의존한다. 이름을 바꾸면 ruleset도 함께 갱신해야 한다.
