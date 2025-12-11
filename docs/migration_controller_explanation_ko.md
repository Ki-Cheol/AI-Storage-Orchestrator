# Migration Controller 코드 설명서 (시험용)

## 파일 위치
메인 파일: pkg/controller/migration.go (전체 359줄)

## 개요
이 코드는 Kubernetes Pod 마이그레이션 컨트롤러입니다. AI 워크로드를 위한 최적화된 Pod 마이그레이션을 관리하며, Persistent Volume을 활용한 체크포인트 기능을 제공합니다.

---

## 주요 구조체 (Structs)

### 1. MigrationController
위치: pkg/controller/migration.go:17

역할: 마이그레이션 작업을 관리하는 메인 컨트롤러

필드 설명:
- k8sClient: Kubernetes API 클라이언트 (Pod, PVC 등을 조작)
- migrations: 현재 진행 중인 마이그레이션 작업들을 저장하는 맵 (key: migrationID, value: MigrationJob)
- migrationsMux: 동시성 제어를 위한 읽기/쓰기 뮤텍스 (여러 고루틴에서 안전하게 접근)
- metrics: 마이그레이션 성능 메트릭스 (성공/실패 횟수, 평균 소요 시간 등)
- checkpointSize: 체크포인트용 Persistent Volume의 기본 크기 (기본값: "1Gi")

왜 필요한가?: 
- 여러 마이그레이션 작업을 동시에 관리
- 스레드 안전성 보장 (mutex 사용)
- 마이그레이션 통계 수집

---

### 2. MigrationJob
위치: pkg/controller/migration.go:26

역할: 개별 마이그레이션 작업의 상태와 정보를 담는 구조체

필드 설명:
- ID: 고유한 마이그레이션 ID (예: "migration-abc12345")
- Request: 마이그레이션 요청 정보 (소스 Pod, 타겟 노드 등)
- Status: 현재 상태 (pending, running, completed, failed, cancelled)
- Details: 마이그레이션 상세 정보 (시작/종료 시간, 리소스 사용량 등)
- StartTime: 마이그레이션 시작 시간
- ctx: 컨텍스트 (타임아웃 제어용)
- cancel: 컨텍스트 취소 함수

왜 필요한가?:
- 각 마이그레이션 작업의 생명주기 추적
- 상태 관리 및 모니터링
- 타임아웃 제어

---

## 주요 함수 (Functions)

### 1. NewMigrationController()
위치: pkg/controller/migration.go:37

```go
func NewMigrationController(k8sClient *k8s.Client) *MigrationController
```

기능: MigrationController 인스턴스를 생성하는 생성자 함수

동작:
- k8sClient를 받아서 새로운 컨트롤러 생성
- migrations 맵 초기화
- metrics 초기화
- checkpointSize를 "1Gi"로 설정

시험 포인트: 
- 생성자 패턴 (Constructor Pattern)
- 초기화 로직

---

### 2. StartMigration()
위치: pkg/controller/migration.go:47

```go
func (mc *MigrationController) StartMigration(req *types.MigrationRequest) (*types.MigrationResponse, error)
```

기능: 새로운 Pod 마이그레이션을 시작하는 함수

동작 과정:
1. 고유 ID 생성: UUID를 사용해 "migration-xxxxx" 형식의 ID 생성
2. 컨텍스트 생성: 타임아웃 설정 (요청에 Timeout이 없으면 기본 10분)
3. MigrationJob 생성: 
   - 상태를 Pending으로 설정
   - 시작 시간 기록
   - Details 초기화
4. 작업 저장: migrations 맵에 저장 (mutex로 동시성 제어)
5. 백그라운드 실행: go mc.executeMigration(job) - 별도 고루틴에서 실행
6. 응답 반환: MigrationResponse 반환 (비동기이므로 즉시 반환)

시험 포인트:
- 비동기 처리 (고루틴 사용)
- 타임아웃 설정
- 동시성 제어 (mutex)
- 즉시 응답 반환 패턴

왜 비동기로 처리하나?: 
- 마이그레이션은 시간이 오래 걸리는 작업
- 클라이언트가 응답을 기다리지 않고 다른 작업 가능

---

### 3. GetMigrationStatus()
위치: pkg/controller/migration.go:86

```go
func (mc *MigrationController) GetMigrationStatus(migrationID string) (*types.MigrationResponse, error)
```

기능: 특정 마이그레이션의 현재 상태를 조회하는 함수

동작 과정:
1. 읽기 락 획득: RLock 사용 (여러 고루틴이 동시에 읽기 가능)
2. 작업 조회: migrations 맵에서 migrationID로 작업 찾기
3. 존재 여부 확인: 없으면 에러 반환
4. 상태 메시지 생성: getStatusMessage()로 사용자 친화적 메시지 생성
5. 응답 반환: MigrationResponse 반환

시험 포인트:
- 읽기 전용 락 (RLock) 사용
- 에러 처리
- 상태 조회 API 패턴

---

### 4. executeMigration()
위치: pkg/controller/migration.go:104

```go
func (mc *MigrationController) executeMigration(job *MigrationJob)
```

기능: 실제 마이그레이션을 수행하는 핵심 함수 (5단계 프로세스)

5단계 마이그레이션 프로세스:

#### Step 1: 컨테이너 상태 캡처 및 메트릭 수집
```go
mc.captureContainerStates(job)
```
- 현재 Pod의 컨테이너 상태 분석
- 리소스 사용량 (CPU, Memory) 수집
- 마이그레이션 대상 컨테이너 식별

#### Step 2: Persistent Volume 체크포인트 생성 (선택적)
```go
if job.Request.PreservePV {
    checkpointPVC = mc.createCheckpoint(job)
}
```
- PreservePV 옵션이 활성화된 경우만 실행
- PVC(Persistent Volume Claim) 생성
- 컨테이너 상태를 영구 저장소에 저장

#### Step 3: 최적화된 Pod 생성
```go
mc.createOptimizedPod(job, checkpointPVC)
```
- 핵심 기능: 실행 중인 컨테이너만 포함하는 최적화된 Pod 생성
- 타겟 노드에 새 Pod 생성
- 새 Pod가 Ready 상태가 될 때까지 대기 (최대 5분)

#### Step 4: 원본 Pod 삭제
```go
mc.deleteOriginalPod(job)
```
- 원본 Pod 삭제
- 실패해도 경고만 로그 (마이그레이션 실패로 처리하지 않음)

#### Step 5: 마이그레이션 후 메트릭 수집
```go
mc.collectPostMigrationMetrics(job)
```
- 30초 대기 후 메트릭 수집 (안정화 시간)
- 최적화된 리소스 사용량 기록

최종: completeMigration() 호출하여 성공 처리

시험 포인트:
- 단계별 마이그레이션 프로세스
- 에러 처리 및 실패 처리
- 최적화된 Pod 생성 (핵심 기능)
- 체크포인트 기능

---

### 5. captureContainerStates()
위치: pkg/controller/migration.go:162

```go
func (mc *MigrationController) captureContainerStates(job *MigrationJob) error
```

기능: 마이그레이션 전 컨테이너 상태와 리소스 메트릭을 수집

동작:
1. 현재 Pod 정보 가져오기
2. 컨테이너 상태 분석 (GetPodContainerStates)
   - 각 컨테이너의 상태 (waiting, running, completed)
   - 재시작 횟수
   - 마이그레이션 대상 여부 (ShouldMigrate)
3. 리소스 메트릭 수집 (GetPodMetrics)
   - CPU 사용량
   - Memory 사용량
   - 실패 시 기본값(0)으로 설정
4. 마이그레이션 대상 컨테이너 개수 로깅

시험 포인트:
- 상태 분석 로직
- 메트릭 수집
- 에러 처리 (경고만 로그, 기본값 설정)

---

### 6. createCheckpoint()
위치: pkg/controller/migration.go:208

```go
func (mc *MigrationController) createCheckpoint(job *MigrationJob) (string, error)
```

기능: 컨테이너 상태를 저장하기 위한 Persistent Volume Claim 생성

동작:
1. 고유한 체크포인트 이름 생성: "checkpoint-{podName}-{timestamp}"
2. k8sClient.CreatePersistentVolumeClaim() 호출
   - 위치: pkg/k8s/client.go:108
   - 기본 크기: 1Gi (mc.checkpointSize)
   - AccessMode: ReadWriteOnce
3. 생성된 PVC 이름 반환

관련 함수:
- 실제 PVC 생성: pkg/k8s/client.go:108 - CreatePersistentVolumeClaim()

시험 포인트:
- PVC 생성 호출
- 고유 이름 생성 패턴
- 에러 처리
- k8sClient를 통한 Kubernetes API 호출

왜 필요한가?: 
- 컨테이너 상태를 영구 저장
- 마이그레이션 실패 시 복구 가능
- 데이터 손실 방지

---

### 7. createOptimizedPod()
위치: pkg/controller/migration.go:223

```go
func (mc *MigrationController) createOptimizedPod(job *MigrationJob, checkpointPVC string) error
```

기능: 핵심 기능 - 실행 중인 컨테이너만 포함하는 최적화된 Pod 생성

동작:
1. 원본 Pod 정보 가져오기
2. k8sClient.CreateOptimizedPod() 호출
   - 위치: pkg/k8s/client.go:144
   - 원본 Pod의 정보를 기반으로 새 Pod 생성
   - 중요: ContainerStates를 기반으로 실행 중인 컨테이너만 포함
   - 타겟 노드에 스케줄링
   - PVC 마운트: checkpointPVC가 있으면 Pod에 마운트
     - Volume 추가: checkpoint-volume (PVC 참조)
     - VolumeMount 추가: /migration-checkpoint 경로
3. 새 Pod가 Ready 상태가 될 때까지 대기 (최대 5분)
4. Ready 확인 후 완료

관련 함수:
- 실제 Pod 생성 및 PVC 마운트: pkg/k8s/client.go:144 - CreateOptimizedPod()

시험 포인트:
- 최적화된 Pod 생성 (핵심 기능)
- 노드 스케줄링
- PVC 마운트 (Volume, VolumeMount)
- Pod Ready 대기
- 타임아웃 처리

왜 최적화인가?:
- 실행 중인 컨테이너만 마이그레이션
- 중지된/완료된 컨테이너는 제외
- 리소스 절약 (CPU 50%, Memory 40% 감소)

---

### 8. deleteOriginalPod()
위치: pkg/controller/migration.go:252

```go
func (mc *MigrationController) deleteOriginalPod(job *MigrationJob) error
```

기능: 원본 Pod 삭제

동작:
- Kubernetes API를 통해 원본 Pod 삭제
- 실패해도 경고만 로그 (마이그레이션은 이미 완료된 상태)

시험 포인트:
- Pod 삭제
- 에러 처리 (경고만, 실패로 처리하지 않음)

---

### 9. collectPostMigrationMetrics()
위치: pkg/controller/migration.go:265

```go
func (mc *MigrationController) collectPostMigrationMetrics(job *MigrationJob) error
```

기능: 마이그레이션 후 리소스 사용량 수집

동작:
1. 30초 대기 (메트릭 안정화)
2. 최적화된 리소스 사용량 계산
   - CPU: 원본의 50% (50% 절감)
   - Memory: 원본의 60% (40% 절감)
3. OptimizedResources에 저장

시험 포인트:
- 메트릭 수집
- 최적화 효과 측정
- 시뮬레이션 로직

---

### 10. Helper 함수들

#### updateJobStatus()
위치: pkg/controller/migration.go:284
- 작업 상태를 안전하게 업데이트 (mutex 사용)

#### failMigration()
위치: pkg/controller/migration.go:290
- 마이그레이션 실패 처리
- 종료 시간 기록
- 소요 시간 계산
- 실패 메트릭 증가

#### completeMigration()
위치: pkg/controller/migration.go:303
- 마이그레이션 성공 처리
- 종료 시간 및 소요 시간 기록
- 성공 메트릭 증가
- 평균 소요 시간 계산
- 리소스 절감률 계산 (CPU, Memory)

#### getStatusMessage()
위치: pkg/controller/migration.go:333
- 상태 코드를 사용자 친화적 메시지로 변환

#### GetMetrics()
위치: pkg/controller/migration.go:351
- 현재 마이그레이션 메트릭스 반환
- 복사본 반환 (원본 보호)

---

## PV/PVC 관련 함수들

### 1. CreatePersistentVolumeClaim()
위치: pkg/k8s/client.go:108

```go
func (c *Client) CreatePersistentVolumeClaim(ctx context.Context, namespace, name string, size string) error
```

기능: 마이그레이션 체크포인트를 위한 Persistent Volume Claim 생성

동작:
1. PVC 스펙 생성:
   - AccessMode: ReadWriteOnce (단일 노드에서 읽기/쓰기)
   - Storage 크기: 요청된 size (기본값: "1Gi")
   - Labels: 
     - app: ai-storage-orchestrator
     - component: migration-checkpoint
2. Kubernetes API 호출: PVC 생성
3. 에러 반환: 생성 실패 시 에러 반환

PVC 스펙 상세:
```go
pvc := &corev1.PersistentVolumeClaim{
    ObjectMeta: metav1.ObjectMeta{
        Name:      name,
        Namespace: namespace,
        Labels: map[string]string{
            "app":       "ai-storage-orchestrator",
            "component": "migration-checkpoint",
        },
    },
    Spec: corev1.PersistentVolumeClaimSpec{
        AccessModes: []corev1.PersistentVolumeAccessMode{
            corev1.ReadWriteOnce,  // RWO: 단일 노드에서만 마운트 가능
        },
        Resources: corev1.ResourceRequirements{
            Requests: corev1.ResourceList{
                corev1.ResourceStorage: resource.MustParse(size),
            },
        },
    },
}
```

시험 포인트:
- PVC 생성 로직
- AccessMode 설정 (ReadWriteOnce)
- 리소스 요청 (Storage 크기)
- Label 설정

왜 ReadWriteOnce인가?:
- 체크포인트는 단일 Pod에서만 사용
- 마이그레이션 중에는 원본 Pod에서만 접근
- 마이그레이션 후에는 새 Pod에서만 접근

---

### 2. PVC를 Pod에 마운트 (CreateOptimizedPod 내부)
위치: pkg/k8s/client.go:187-195

기능: 생성된 PVC를 최적화된 Pod에 마운트

동작 과정:

#### Step 1: Volume 정의 (Pod Spec에 추가)
```go
if checkpointPVC != "" {
    newPod.Spec.Volumes = append(newPod.Spec.Volumes, corev1.Volume{
        Name: "checkpoint-volume",
        VolumeSource: corev1.VolumeSource{
            PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
                ClaimName: checkpointPVC,
            },
        },
    })
}
```

#### Step 2: Volume Mount (컨테이너에 마운트)
```go
if checkpointPVC != "" {
    container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
        Name:      "checkpoint-volume",
        MountPath: "/migration-checkpoint",  // 컨테이너 내부 마운트 경로
    })
}
```

마운트 구조:
- Volume 이름: checkpoint-volume
- 마운트 경로: /migration-checkpoint
- PVC 이름: checkpoint-{podName}-{timestamp}

시험 포인트:
- Volume 정의
- VolumeMount 설정
- PVC를 Pod에 연결하는 방법

왜 필요한가?:
- 컨테이너 상태를 영구 저장소에 저장
- 마이그레이션 후 새 Pod에서 체크포인트 데이터 접근 가능
- 데이터 손실 방지

---

### 3. RBAC 권한 설정
위치: deployments/cluster-orchestrator.yaml:18

기능: PVC 생성/관리를 위한 Kubernetes 권한 설정

권한 내용:
```yaml
rules:
- apiGroups: [""]
  resources: ["pods", "persistentvolumeclaims", "nodes"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

필요한 권한:
- persistentvolumeclaims: PVC 리소스 접근
- create: PVC 생성
- get, list, watch: PVC 조회 및 모니터링
- update, patch: PVC 수정
- delete: PVC 삭제 (선택적)

시험 포인트:
- RBAC 설정
- 필요한 권한 이해

---

## PV/PVC 관련 핵심 개념

### 1. Persistent Volume Claim (PVC)
- 역할: 사용자가 스토리지를 요청하는 리소스
- 생성 위치: pkg/k8s/client.go:108 (CreatePersistentVolumeClaim)
- 사용 목적: 마이그레이션 체크포인트 저장

### 2. PVC 생성 프로세스
1. createCheckpoint() 호출 (migration.go:208)
2. CreatePersistentVolumeClaim() 호출 (k8s/client.go:108)
3. Kubernetes가 자동으로 PV를 바인딩
4. PVC 이름 반환

### 3. PVC 마운트 프로세스
1. createOptimizedPod() 호출 시 checkpointPVC 파라미터 전달
2. Pod Spec에 Volume 추가 (k8s/client.go:187)
3. 컨테이너에 VolumeMount 추가 (k8s/client.go:173)
4. Pod 생성 시 PVC가 자동으로 마운트됨

### 4. AccessMode: ReadWriteOnce (RWO)
- 의미: 단일 노드에서만 읽기/쓰기 가능
- 이유: 체크포인트는 한 번에 하나의 Pod에서만 사용
- 제한: 동시에 여러 Pod에서 마운트 불가

### 5. 체크포인트 이름 생성 규칙
```go
checkpointName := fmt.Sprintf("checkpoint-%s-%d", job.Request.PodName, time.Now().Unix())
```
- 형식: checkpoint-{podName}-{timestamp}
- 예시: checkpoint-tensorflow-pod-1699123456
- 고유성 보장: 타임스탬프 사용

### 6. PVC 라이프사이클
1. 생성: PreservePV=true 옵션 시 생성
2. 마운트: 최적화된 Pod에 마운트
3. 사용: 컨테이너 상태 저장
4. 보존: 마이그레이션 완료 후에도 유지 (수동 삭제 필요)

---

## 핵심 개념 및 시험 포인트

### 1. 동시성 제어 (Concurrency Control)
- 문제: 여러 고루틴이 동시에 migrations 맵에 접근
- 해결: sync.RWMutex 사용
  - 쓰기: Lock() / Unlock()
  - 읽기: RLock() / RUnlock() (여러 고루틴 동시 읽기 가능)

### 2. 비동기 처리 (Asynchronous Processing)
- StartMigration()은 즉시 반환
- 실제 마이그레이션은 go mc.executeMigration(job)로 백그라운드 실행
- 이유: 마이그레이션은 시간이 오래 걸리는 작업

### 3. 타임아웃 제어
- Context를 사용한 타임아웃 관리
- 기본값: 10분
- 요청에서 Timeout 지정 가능

### 4. 최적화된 Pod 생성 (핵심 기능)
- 핵심: 실행 중인 컨테이너만 마이그레이션
- 리소스 절감 효과:
  - CPU: 50% 절감
  - Memory: 40% 절감
- 중지된/완료된 컨테이너는 제외

### 5. 체크포인트 기능
- Persistent Volume을 활용한 상태 저장
- PreservePV 옵션으로 활성화
- 마이그레이션 실패 시 복구 가능

### 6. 5단계 마이그레이션 프로세스
1. 컨테이너 상태 캡처
2. 체크포인트 생성 (선택적)
3. 최적화된 Pod 생성
4. 원본 Pod 삭제
5. 메트릭 수집

### 7. 에러 처리 전략
- 중요 단계: 실패 시 마이그레이션 중단
- 부가 단계: 실패해도 경고만 로그 (원본 Pod 삭제, 메트릭 수집)

### 8. 메트릭 수집
- 마이그레이션 전/후 리소스 사용량 비교
- 성공/실패 횟수 추적
- 평균 소요 시간 계산
- 리소스 절감률 계산

---

## 시험 예상 질문과 답변

### Q1: MigrationController의 역할은 무엇인가요?
A: Kubernetes Pod 마이그레이션을 관리하는 컨트롤러입니다. AI 워크로드를 위한 최적화된 마이그레이션을 제공하며, 실행 중인 컨테이너만 마이그레이션하여 리소스를 절감합니다.

### Q2: StartMigration() 함수는 어떻게 동작하나요?
A: 
1. 고유한 마이그레이션 ID 생성
2. 타임아웃 설정된 컨텍스트 생성
3. MigrationJob 생성 및 저장
4. 백그라운드 고루틴에서 executeMigration() 실행
5. 즉시 응답 반환 (비동기 처리)

### Q3: 최적화된 Pod 생성이란 무엇인가요?
A: 실행 중인 컨테이너만 포함하여 새 Pod를 생성하는 기능입니다. 중지된 컨테이너는 제외하여 CPU 50%, Memory 40%를 절감합니다.

### Q4: executeMigration()의 5단계 프로세스를 설명하세요.
A:
1. 컨테이너 상태 캡처 및 메트릭 수집
2. Persistent Volume 체크포인트 생성 (선택적)
3. 최적화된 Pod 생성 (타겟 노드)
4. 원본 Pod 삭제
5. 마이그레이션 후 메트릭 수집

### Q5: 동시성 제어는 어떻게 하나요?
A: sync.RWMutex를 사용합니다. 쓰기 작업은 Lock(), 읽기 작업은 RLock()을 사용하여 여러 고루틴이 동시에 읽을 수 있도록 합니다.

### Q6: 체크포인트 기능은 무엇인가요?
A: Persistent Volume Claim을 생성하여 컨테이너 상태를 영구 저장하는 기능입니다. PreservePV 옵션으로 활성화하며, 마이그레이션 실패 시 복구에 사용됩니다.

### Q7: 왜 비동기로 처리하나요?
A: 마이그레이션은 시간이 오래 걸리는 작업이므로, 클라이언트가 응답을 기다리지 않고 다른 작업을 수행할 수 있도록 비동기로 처리합니다.

### Q8: 메트릭 수집은 언제 하나요?
A: 
- 마이그레이션 전: captureContainerStates()에서 수집
- 마이그레이션 후: collectPostMigrationMetrics()에서 수집 (30초 대기 후)

### Q9: PVC는 어디서 생성되나요?
A: pkg/k8s/client.go:108의 CreatePersistentVolumeClaim() 함수에서 생성됩니다. createCheckpoint() 함수가 이를 호출합니다.

### Q10: PVC는 어떻게 Pod에 마운트되나요?
A: 
1. Pod Spec에 Volume 추가 (PVC 참조)
2. 컨테이너에 VolumeMount 추가 (/migration-checkpoint 경로)
3. Pod 생성 시 자동으로 마운트됨

---

## 핵심 키워드

- Pod 마이그레이션: Kubernetes Pod를 한 노드에서 다른 노드로 이동
- 최적화된 Pod: 실행 중인 컨테이너만 포함하는 Pod
- 체크포인트: Persistent Volume에 상태 저장
- PVC (Persistent Volume Claim): 스토리지 요청 리소스
- PV (Persistent Volume): 실제 스토리지 리소스
- ReadWriteOnce (RWO): 단일 노드에서만 읽기/쓰기 가능한 AccessMode
- Volume Mount: PVC를 Pod의 컨테이너에 마운트
- 동시성 제어: Mutex를 사용한 스레드 안전성
- 비동기 처리: 고루틴을 사용한 백그라운드 실행
- 리소스 절감: CPU 50%, Memory 40% 절감
- 5단계 프로세스: 캡처 → 체크포인트 → 생성 → 삭제 → 메트릭

---

## 시스템 시작 흐름

### 1. 프로그램 시작점
위치: cmd/main.go:20

main() 함수가 프로그램의 진입점입니다.

### 2. 초기화 순서

#### Step 1: Kubernetes Client 초기화
위치: cmd/main.go:30
```go
k8sClient, err := k8s.NewClient(*kubeconfig)
```
- kubeconfig 파일 경로를 받아서 Kubernetes 클라이언트 생성
- 클러스터 내부에서 실행 시 kubeconfig는 비어있을 수 있음 (in-cluster config 사용)
- 실패 시 프로그램 종료

#### Step 2: MigrationController 초기화
위치: cmd/main.go:37
```go
migrationController := controller.NewMigrationController(k8sClient)
```
- k8sClient를 전달하여 MigrationController 생성
- migrations 맵, metrics 초기화
- checkpointSize를 "1Gi"로 설정

#### Step 3: API Handler 초기화
위치: cmd/main.go:41-42
```go
apiHandler := apis.NewHandler(migrationController)
router := apiHandler.SetupRoutes()
```
- migrationController를 전달하여 API Handler 생성
- HTTP 라우트 설정:
  - POST /api/v1/migrations: 마이그레이션 시작
  - GET /api/v1/migrations/:id: 마이그레이션 상세 조회
  - GET /api/v1/migrations/:id/status: 마이그레이션 상태 조회
  - GET /api/v1/metrics: 메트릭 조회
  - GET /health: 헬스 체크

#### Step 4: HTTP 서버 시작
위치: cmd/main.go:57-61
```go
go func() {
    if err := router.Run(":" + *port); err != nil {
        log.Fatalf("Failed to start HTTP server: %v", err)
    }
}()
```
- 기본 포트: 8080 (--port 플래그로 변경 가능)
- 별도 고루틴에서 HTTP 서버 실행
- 서버가 준비되면 마이그레이션 요청을 받을 수 있음

#### Step 5: Graceful Shutdown 대기
위치: cmd/main.go:67
```go
<-quit
```
- SIGINT 또는 SIGTERM 신호를 받을 때까지 대기
- 신호 수신 시 정상 종료

### 전체 시작 흐름 요약
1. main() 함수 시작 (cmd/main.go:20)
2. Kubernetes Client 생성 (cmd/main.go:30)
3. MigrationController 생성 (cmd/main.go:37)
4. API Handler 생성 및 라우트 설정 (cmd/main.go:41-42)
5. HTTP 서버 시작 (cmd/main.go:57)
6. 요청 대기 상태로 진입

---

## 마이그레이션 요청 처리 흐름

### 1. HTTP 요청 수신
위치: pkg/apis/handler.go:59

클라이언트가 POST /api/v1/migrations 요청을 보냅니다.

요청 예시:
```json
{
  "pod_name": "tensorflow-pod",
  "pod_namespace": "default",
  "source_node": "node-1",
  "target_node": "node-2",
  "preserve_pv": true,
  "timeout": 600
}
```

### 2. 요청 파싱 및 검증
위치: pkg/apis/handler.go:60-77

#### Step 2-1: JSON 파싱
```go
var req types.MigrationRequest
if err := c.ShouldBindJSON(&req); err != nil {
    // 에러 반환
}
```

#### Step 2-2: 필수 필드 검증
위치: pkg/apis/handler.go:71
```go
if err := h.validateMigrationRequest(&req); err != nil {
    // 에러 반환
}
```
검증 항목:
- pod_name: 필수
- pod_namespace: 필수
- source_node: 필수
- target_node: 필수
- source_node != target_node
- timeout >= 0

#### Step 2-3: 기본값 설정
위치: pkg/apis/handler.go:80-82
```go
if req.Timeout == 0 {
    req.Timeout = 600 // 10분 기본값
}
```

### 3. 마이그레이션 시작
위치: pkg/apis/handler.go:85
```go
response, err := h.migrationController.StartMigration(&req)
```

#### Step 3-1: StartMigration() 호출
위치: pkg/controller/migration.go:47

1. 마이그레이션 ID 생성: "migration-{uuid}"
2. 타임아웃 컨텍스트 생성
3. MigrationJob 생성 및 저장
4. 백그라운드 고루틴에서 executeMigration() 시작
5. 즉시 MigrationResponse 반환 (상태: pending)

#### Step 3-2: HTTP 응답 반환
위치: pkg/apis/handler.go:94
```go
c.JSON(http.StatusAccepted, response)
```
- 상태 코드: 202 Accepted (비동기 작업 시작)
- 응답 본문: MigrationResponse (migration_id 포함)

### 4. 백그라운드 마이그레이션 실행
위치: pkg/controller/migration.go:104

executeMigration() 함수가 별도 고루틴에서 실행됩니다.

#### Step 4-1: 상태를 Running으로 변경
위치: pkg/controller/migration.go:116
```go
mc.updateJobStatus(job, types.MigrationStatusRunning)
```

#### Step 4-2: Step 1 - 컨테이너 상태 캡처
위치: pkg/controller/migration.go:119
```go
mc.captureContainerStates(job)
```
- 현재 Pod 정보 가져오기
- 컨테이너 상태 분석
- 리소스 메트릭 수집
- 실패 시 마이그레이션 중단

#### Step 4-3: Step 2 - 체크포인트 생성 (선택적)
위치: pkg/controller/migration.go:126-135
```go
if job.Request.PreservePV {
    checkpointPVC, err = mc.createCheckpoint(job)
    // 실패 시 마이그레이션 중단
}
```
- PreservePV가 true인 경우만 실행
- PVC 생성 (pkg/k8s/client.go:108)
- 실패 시 마이그레이션 중단

#### Step 4-4: Step 3 - 최적화된 Pod 생성
위치: pkg/controller/migration.go:138
```go
mc.createOptimizedPod(job, checkpointPVC)
```
- 원본 Pod 정보 가져오기
- 실행 중인 컨테이너만 필터링
- 타겟 노드에 새 Pod 생성 (pkg/k8s/client.go:144)
- PVC 마운트 (있는 경우)
- Pod Ready 대기 (최대 5분)
- 실패 시 마이그레이션 중단

#### Step 4-5: Step 4 - 원본 Pod 삭제
위치: pkg/controller/migration.go:144
```go
mc.deleteOriginalPod(job)
```
- 원본 Pod 삭제
- 실패해도 경고만 로그 (마이그레이션은 계속 진행)

#### Step 4-6: Step 5 - 메트릭 수집
위치: pkg/controller/migration.go:150
```go
mc.collectPostMigrationMetrics(job)
```
- 30초 대기
- 최적화된 리소스 사용량 계산
- 실패해도 경고만 로그

#### Step 4-7: 마이그레이션 완료
위치: pkg/controller/migration.go:156
```go
mc.completeMigration(job)
```
- 상태를 Completed로 변경
- 종료 시간 기록
- 메트릭 업데이트

### 전체 마이그레이션 요청 처리 흐름 요약

1. HTTP 요청 수신 (POST /api/v1/migrations)
   - 위치: pkg/apis/handler.go:59
2. 요청 파싱 및 검증
   - 위치: pkg/apis/handler.go:60-77
3. StartMigration() 호출
   - 위치: pkg/controller/migration.go:47
   - 즉시 응답 반환 (202 Accepted)
4. 백그라운드에서 executeMigration() 실행
   - 위치: pkg/controller/migration.go:104
   - Step 1: 컨테이너 상태 캡처 (migration.go:119)
   - Step 2: 체크포인트 생성 (migration.go:128, 선택적)
   - Step 3: 최적화된 Pod 생성 (migration.go:138)
   - Step 4: 원본 Pod 삭제 (migration.go:144)
   - Step 5: 메트릭 수집 (migration.go:150)
   - 완료 처리 (migration.go:156)

### 상태 조회 흐름

클라이언트가 GET /api/v1/migrations/:id 요청을 보내면:

1. 요청 수신
   - 위치: pkg/apis/handler.go:98
2. GetMigrationStatus() 호출
   - 위치: pkg/controller/migration.go:86
3. migrations 맵에서 작업 조회
4. MigrationResponse 반환
   - 현재 상태 (pending, running, completed, failed)
   - 상세 정보 (시작 시간, 소요 시간, 리소스 사용량 등)

---

## 전체 시스템 아키텍처 흐름도

```
[클라이언트]
    |
    | POST /api/v1/migrations
    v
[HTTP 서버] (cmd/main.go:57)
    |
    | 요청 라우팅
    v
[API Handler] (pkg/apis/handler.go:59)
    |
    | 요청 검증 및 파싱
    v
[MigrationController.StartMigration()] (pkg/controller/migration.go:47)
    |
    | MigrationJob 생성 및 저장
    | 백그라운드 고루틴 시작
    |
    +---> [executeMigration()] (pkg/controller/migration.go:104)
            |
            | Step 1: captureContainerStates()
            |   └─> [k8sClient.GetPod()]
            |   └─> [k8sClient.GetPodContainerStates()]
            |   └─> [k8sClient.GetPodMetrics()]
            |
            | Step 2: createCheckpoint() (선택적)
            |   └─> [k8sClient.CreatePersistentVolumeClaim()]
            |
            | Step 3: createOptimizedPod()
            |   └─> [k8sClient.GetPod()]
            |   └─> [k8sClient.CreateOptimizedPod()]
            |       └─> PVC 마운트 (있는 경우)
            |   └─> [k8sClient.WaitForPodReady()]
            |
            | Step 4: deleteOriginalPod()
            |   └─> [k8sClient.DeletePod()]
            |
            | Step 5: collectPostMigrationMetrics()
            |
            v
        [completeMigration()]
            └─> 상태를 Completed로 변경
            └─> 메트릭 업데이트
```

---

## 주요 파일 위치 정리

- 프로그램 시작점: cmd/main.go:20 (main 함수)
- API 핸들러: pkg/apis/handler.go
- 마이그레이션 컨트롤러: pkg/controller/migration.go
- Kubernetes 클라이언트: pkg/k8s/client.go
- 타입 정의: pkg/types/migration.go
- RBAC 설정: deployments/cluster-orchestrator.yaml

---

## 시험 대비 체크리스트

1. main() 함수에서 초기화 순서 이해
2. HTTP 요청이 어떻게 처리되는지 이해
3. StartMigration()의 비동기 처리 방식 이해
4. executeMigration()의 5단계 프로세스 암기
5. 각 단계에서 호출되는 함수와 위치 파악
6. PVC 생성 및 마운트 과정 이해
7. 에러 처리 전략 이해
8. 동시성 제어 방법 이해



