# AI Storage Cluster Orchestrator

**논문 기반 Kubernetes Pod 마이그레이션 오케스트레이터**

## 개요

이 프로젝트는 **"Kubernetes에서 Persistent Volume을 사용한 최적화된 컨테이너 Pod 마이그레이션"** 연구 논문을 기반으로 구현된 AI Storage Cluster Orchestrator입니다.

### 🎯 주요 목표

- **CPU 사용량 50% 절감** - 완료된 컨테이너 제외를 통한 리소스 최적화
- **메모리 사용량 40% 절감** - 불필요한 컨테이너 메모리 절약  
- **콜드 스타트 시간 50% 단축** - PV 기반 체크포인트로 빠른 복원
- **무중단 마이그레이션** - Persistent Volume을 활용한 상태 보존

### 🏗️ 아키텍처

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Control       │    │   Compute       │    │   Storage       │  
│   Plane         │    │   Nodes         │    │   Nodes         │
│                 │    │                 │    │                 │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │Orchestrator │ │    │ │    Pods     │ │    │ │     PVs     │ │
│ │             │ │    │ │             │ │    │ │             │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

### 🔬 최적화된 3단계 마이그레이션

1. **상태 캡처**: 컨테이너별 실행 상태 분석 (waiting/running/completed)
2. **체크포인트 저장**: Persistent Volume에 컨테이너 상태 저장
3. **최적화된 재배포**: 실행 중인 컨테이너만으로 새 Pod 생성

## 🧭 오케스트레이션 관점에서 본 시스템 동작

- **API 게이트웨이 역할의 HTTP 서버** (`cmd/main.go`, `pkg/apis/handler.go`)
  - `POST /api/v1/migrations` 요청을 수신하면 JSON 검증, 타임아웃 기본값 설정 후 컨트롤러에 위임
  - `GET /api/v1/migrations/:id`와 `/metrics` 엔드포인트로 상태·메트릭을 실시간 노출
- **MigrationController** (`pkg/controller/migration.go`)
  - 요청마다 `MigrationJob`을 생성해 내부 맵에 저장하고 고루틴으로 실행
  - 상태 변경은 `sync.RWMutex`로 보호되어 다중 요청 시에도 일관성 유지
  - 진행 현황, PV 체크포인트 경로, 새 Pod 이름, 리소스 사용량을 `MigrationDetails`에 누적
- **Kubernetes 연동 레이어** (`pkg/k8s/client.go`)
  - Pod 조회, 컨테이너 상태 분석, PVC 생성, 최적화된 Pod 생성, Ready 대기, 메트릭 수집까지 모든 실제 연산을 Kubernetes API/metrics API로 수행
  - 오케스트레이터는 Control Plane에서 명령만 내리고 실제 작업은 클러스터가 처리하므로, 장애 노드와 독립적으로 동작
- **운영 관점 KPI 노출**
  - `MigrationMetrics`에 전체/성공/실패 횟수, 평균 소요시간, 실측 기반 CPU·메모리 절감율 저장
  - 외부 모니터링 시스템이 API를 폴링하면 즉시 운영 현황을 파악 가능

## 🔄 실제 마이그레이션 파이프라인 (실행 기반)

1. **요청 수신 및 검증**
   - `handler.createMigration()`이 JSON을 파싱하고 필수 필드·노드 중복·타임아웃 양수 여부를 검사
   - 통과 시 `StartMigration()` 호출과 동시에 HTTP 202 응답 반환 (비동기 수행)
2. **작업 생성과 상태 관리**
   - `StartMigration()`은 UUID 기반 ID를 생성하고 `context.WithTimeout`으로 전체 작업 타임아웃을 설정
   - `MigrationJob`이 `migrations` 맵에 저장되어 이후 조회 API가 동일한 상태를 반환
3. **컨테이너 상태 캡처**
   - `captureContainerStates()`가 원본 Pod의 `ContainerStatuses`를 읽어 `running`/`waiting`/`completed` 판별
   - 동시에 `metrics.k8s.io`로부터 실제 CPU(cores)/메모리(bytes)를 수집해 `OriginalResources`에 저장
4. **체크포인트 PVC 생성 (옵션 PreservePV=true)**
   - `createCheckpoint()` → `k8sClient.CreatePersistentVolumeClaim()` 호출
   - AccessMode=RWO, 기본 크기 1Gi, `migration-checkpoint` 라벨로 생성되어 새 Pod가 바로 붙을 수 있음
5. **최적화된 Pod 생성**
   - `CreateOptimizedPod()`가 실행 대상 컨테이너만 남긴 새 스펙을 만들고, 필요 시 `checkpoint-volume`을 마운트
   - 생성 직후 `WaitForPodReady()`로 최대 5분까지 Ready 상태를 감시해 실제 서비스 전환을 보장
   - 새 Pod 이름이 `MigrationDetails.NewPodName`에 저장되어 후속 메트릭 수집에 사용
6. **원본 Pod 제거**
   - `deleteOriginalPod()`가 그레이스풀 삭제를 시도 (실패해도 경고만 출력해 데이터 손실 없이 진행)
7. **실측 메트릭 수집**
   - 30초 안정화 후 `collectPostMigrationMetrics()`가 새 Pod의 실제 CPU/메모리를 다시 조회
   - 조회 실패 시에만 시뮬레이션 값(50%/40%)을 임시로 기록하여 운영자가 원인을 추적할 수 있도록 로그 남김
8. **완료 및 메트릭 업데이트**
   - `completeMigration()`이 종료 시간·소요 시간 기록, 총/성공 횟수 증가, 실측 절감율 계산
   - 이후 `GetMetrics()` API가 최신 KPI를 제공

### 운영 시사점
- 오케스트레이터는 **API 호출 → 컨트롤러 → Kubernetes API** 흐름으로 구성되어 있으며, 각 단계가 로그와 메트릭을 남겨 재현 가능
- PVC 기반 상태 보존, 실행 컨테이너 선별, Ready 검증, 실측 메트릭 보고까지 모두 실제 클러스터에서 수행되는 절차만 포함되어 있어 논문 의존 없이 현장 적용 가능

## 🚀 빠른 시작

### 사전 요구사항

- **Kubernetes**: 1.25+
- **Go**: 1.21+
- **Docker**: 최신 버전
- **kubectl**: 클러스터 접근 권한

### 1. 노드 라벨링

```bash
# Control Plane 노드
kubectl label nodes <master-node> layer=orchestration
kubectl label nodes <master-node> node-role.kubernetes.io/control-plane=

# Worker 노드 (Compute)
kubectl label nodes <worker-node> layer=compute  
kubectl label node <worker-node> node-role.kubernetes.io/worker=

# Storage 노드
kubectl label nodes <storage-node> layer=storage
kubectl label node <storage-node> node-role.kubernetes.io/worker=
```

### 2. 빌드 및 배포

```bash
# 저장소 클론
git clone https://github.com/KETI-AI-Storage/AI-Storage-API-Server.git
cd ai-storage-orchestrator

# 빌드 실행
./scripts/build.sh

# Kubernetes에 배포
./scripts/deploy.sh
```

### 3. 서비스 확인

```bash
# 배포 상태 확인
kubectl get pods -n kube-system -l app=ai-storage-orchestrator

# 포트 포워딩
kubectl port-forward -n kube-system svc/ai-storage-orchestrator 8080:8080

# Health Check
curl http://localhost:8080/health
```

## 📡 API 사용법

### Pod 마이그레이션 시작

```bash
curl -X POST http://localhost:8080/api/v1/migrations \
  -H "Content-Type: application/json" \
  -d '{
    "pod_name": "example-pod",
    "pod_namespace": "default",
    "source_node": "worker-1", 
    "target_node": "worker-2",
    "preserve_pv": true,
    "timeout": 600
  }'
```

### 마이그레이션 상태 조회

```bash
curl http://localhost:8080/api/v1/migrations/{migration-id}
```

### 성능 메트릭 확인

```bash
curl http://localhost:8080/api/v1/metrics
```

## 📊 성능 최적화 기대 효과

K8s 기준 대비 성능 개선:

| 메트릭 | 기존 K8s 방식 | 최적화된 방식 | K8s 기준 개선율 |
|--------|---------------|----------------|-----------------|
| CPU 사용량 | 100% | 50% | **50% 절감** |
| 메모리 사용량 | 100% | 60% | **40% 절감** |  
| 콜드 스타트 시간 | 100% | 50% | **50% 단축** |

## 🛠️ 고급 기능

### 배치 마이그레이션

여러 Pod를 순차적으로 마이그레이션:

```bash
# 스크립트 예시 (USAGE.md 참조)
for pod in app-1 app-2 app-3; do
  # 마이그레이션 API 호출
done
```

### 조건부 마이그레이션

리소스 사용량이 높은 Pod를 자동으로 마이그레이션:

```bash
# 높은 CPU 사용률의 Pod 자동 마이그레이션
kubectl top pods | awk '$2 > 100 {print $1}' | xargs -I {} ./migrate-pod.sh {}
```

### 모니터링 및 알림

실시간 성능 모니터링:

```bash
# 메트릭 수집 및 알림
watch -n 60 'curl -s http://localhost:8080/api/v1/metrics | jq'
```

## 📁 프로젝트 구조

```
ai-storage-orchestrator/
├── cmd/
│   └── main.go                    # 메인 애플리케이션
├── pkg/
│   ├── apis/
│   │   └── handler.go            # HTTP API 핸들러
│   ├── controller/
│   │   └── migration.go          # 마이그레이션 컨트롤러  
│   ├── k8s/
│   │   └── client.go             # Kubernetes 클라이언트
│   └── types/
│       └── migration.go          # 데이터 타입 정의
├── deployments/
│   └── cluster-orchestrator.yaml # K8s 배포 매니페스트
├── scripts/
│   ├── build.sh                  # 빌드 스크립트
│   ├── deploy.sh                 # 배포 스크립트
│   ├── ai_migration_compare.sh   # AI 컨테이너 성능 비교 (공인 인증)
│   └── benchmark-migration.sh    # 일반 마이그레이션 벤치마크
├── Dockerfile                     # 컨테이너 이미지 정의
├── USAGE.md                      # 상세 사용법 가이드
└── README.md                     # 이 파일
```

## 🔍 주요 구현 특징

### 1. 컨테이너 상태 기반 최적화

```go
// 최적화 핵심: 컨테이너 상태별 처리
type ContainerState struct {
    Name          string `json:"name"`
    State         string `json:"state"`         // waiting, running, completed
    ShouldMigrate bool   `json:"should_migrate"` // 마이그레이션 여부 결정
}
```

### 2. Persistent Volume 활용

- Pod 생명주기와 독립적인 데이터 보존
- 체크포인트 기반 빠른 상태 복원
- 노드 간 안전한 상태 이동

### 3. RESTful API

- 간편한 HTTP API 인터페이스
- 실시간 마이그레이션 상태 조회
- 성능 메트릭 수집 및 모니터링

## 🧪 테스트 및 검증

### AI 컨테이너 마이그레이션 성능 비교 (공인 인증)

```bash
# AI 학습 컨테이너 CPU 절감율 비교 테스트
./scripts/ai_migration_compare.sh --source-node worker1 --target-node worker2
```

**특징:**
- TensorFlow AI 워크로드 기반 실제 테스트
- K8s 네이티브 vs AI Orchestrator 정확한 비교
- 공인 인증서 형태의 결과 출력 (영문)
- CPU/메모리 절감율 정밀 측정
- KETI 공식 인증서 발급

### 성능 벤치마크

```bash
# 일반 마이그레이션 성능 측정
./scripts/benchmark-migration.sh --source-node node1 --target-node node2
```

### 단위 테스트

```bash
go test ./pkg/...
```

## 📚 문서

- **[USAGE.md](USAGE.md)** - 상세 사용법 및 고급 기능 가이드
- **API 문서** - Swagger/OpenAPI 스펙 (예정)
- **개발자 가이드** - 기여 방법 및 개발 환경 설정

## 🤝 기여

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## 📄 라이선스

이 프로젝트는 Apache 2.0 라이선스 하에 배포됩니다.

## 🙏 Acknowledgements

This work was supported by the Institute of Information & Communications Technology Planning & Evaluation(IITP) grant funded by the Korea government(MSIT) (No.RS-2024-00461572, Development of High-efficiency Parallel Storage SW Technology Optimized for AI Computational Accelerators)

---

**Developed by KETI (Korea Electronics Technology Institute)**

참고 연구: "Optimized Container Pod Migration using Persistent Volume in Kubernetes"# AI-Storage-Orchestrator
