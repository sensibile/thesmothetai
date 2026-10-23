# 리모트 저장소 운영

GitHub 저장소 `sensibile/thesmothetai`의 설정과 PR 절차를 기록한다. 실제 강제는 아래 GitHub 설정이 담당하며, 이 문서가 권한을 부여하거나 제한하지 않는다. 설정은 `sensibile/taeguk`의 [리모트 저장소 운영](https://github.com/sensibile/taeguk/blob/main/docs/repository-operations.md)과 같게 맞췄다.

## PR 플로우

1. 작업 브랜치에서 변경하고 `go vet ./...`, `go test ./...`를 실행한다.
2. PR을 만든다. `main`에는 직접 push할 수 없다.
3. `@codex review` 댓글로 Codex 리뷰를 요청한다.
4. PR head 커밋에 대한 Codex 리뷰가 완료된 것을 확인한다. 근거는 `chatgpt-codex-connector[bot]` 요약 댓글의 상태(`completed`와 리뷰 커밋 SHA)다. 리뷰 후 새 커밋을 push하면 3단계부터 다시 요청한다.
5. Codex 리뷰가 완료되고 리뷰 스레드가 모두 해소되고 CodeQL 검사를 통과하면 squash 머지한다. Codex 리뷰는 GitHub 필수 체크로 강제되지 않으므로 4단계 확인을 생략하지 않는다.

CodeQL default setup이 PR을 분석하지 않아 머지가 막히면 빈 커밋을 push해 분석을 다시 실행한다. 저장소 첫 설정 직후 연 PR에서 한 번 발생했다.

## GitHub 설정

2026-10-23에 적용하고 API 조회로 반영을 확인했다.

- ruleset `main protection`(id 25699905): 기본 브랜치에 적용한다. PR 필수, 필요 승인 0, 리뷰 스레드 해소 필수, squash 머지만 허용, CodeQL error 또는 high 이상 보안 경고 시 머지 차단, 브랜치 삭제와 force push 금지, bypass 대상 없음.
- 머지 방식: squash만 허용한다. 커밋 제목은 PR 제목, 본문은 PR 본문을 쓴다. 머지 후 브랜치를 자동 삭제한다. auto-merge를 허용한다.
- 보안: Dependabot alerts와 security updates, CodeQL default setup(go), secret scanning과 push protection.
- Codex 리뷰: ChatGPT Codex Connector 앱의 저장소 접근과 Codex cloud 설정의 Code review를 켰다. 이 설정은 GitHub가 아니라 ChatGPT 계정에서 관리한다.

필요 승인을 0으로 둔 이유는 작업자와 PR 작성자가 같은 계정이고 GitHub가 자기 PR 승인을 허용하지 않기 때문이다.

## 아직 걸지 않은 규칙

`required_status_checks`는 CI workflow가 생긴 뒤 그 job 이름으로 추가한다. 존재하지 않는 체크를 필수로 걸면 머지가 영구히 막힌다.
