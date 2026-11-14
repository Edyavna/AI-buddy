import React from 'react';
import { useSearchParams } from 'react-router-dom';
import { EmptyState, InlineError, Spinner, StatusBadge } from '@/shared/ui';
import { useCheckpointConfirmation, usePlan, useUpdateStep } from './hooks';
import { StepCard } from './StepCard';
import { RecommendationCard } from './RecommendationCard';
import type { Step, StepTarget } from './schemas';
import styles from './PlanPage.module.css';

const DOT_STATUS_CLASS: Record<Step['status'], string | undefined> = {
  completed: styles.stepDotCompleted,
  in_progress: styles.stepDotInProgress,
  blocked: styles.stepDotBlocked,
  not_started: styles.stepDotNotStarted,
};

const DOT_STATUS_LABEL: Record<Step['status'], string> = {
  completed: 'выполнен',
  in_progress: 'в работе',
  blocked: 'заблокирован',
  not_started: 'не начат',
};

function ChevronIcon({ direction }: { direction: 'left' | 'right' }) {
  return (
    <svg className={styles.carouselChevron} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d={direction === 'left' ? 'm14.5 5-7 7 7 7' : 'm9.5 5 7 7-7 7'}
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export const PlanPage: React.FC = () => {
  const plan = usePlan();
  const updateStep = useUpdateStep();
  const updateConfirmation = useCheckpointConfirmation();
  const [searchParams, setSearchParams] = useSearchParams();
  const stepIdFromUrl = searchParams.get('stepId');
  const [selectedStepId, setSelectedStepId] = React.useState<string | null>(stepIdFromUrl);
  const [navigationDirection, setNavigationDirection] = React.useState<'forward' | 'backward'>('forward');

  React.useEffect(() => {
    if (stepIdFromUrl) {
      setSelectedStepId(stepIdFromUrl);
    }
  }, [stepIdFromUrl]);

  if (plan.isPending) {
    return (
      <div className={styles.loadingState} role="status" aria-label="Загрузка плана адаптации">
        <Spinner size="lg" />
      </div>
    );
  }

  if (!plan.data) {
    return (
      <InlineError
        className={styles.errorState ?? ''}
        message={plan.error instanceof Error ? plan.error.message : 'Не удалось загрузить план'}
        onRetry={() => void plan.refetch()}
        isRetrying={plan.isRefetching}
      />
    );
  }

  const data = plan.data;
  const completedCount = data.steps.filter((step) => step.status === 'completed').length;
  const nextStep = data.steps.find((step) => step.id === data.next_step_id);
  const currentStep =
    data.steps.find((step) => step.id === (selectedStepId ?? stepIdFromUrl)) ??
    nextStep ??
    data.steps[0];
  const currentIndex = currentStep ? data.steps.findIndex((step) => step.id === currentStep.id) : -1;
  const allRemainingBlocked =
    !nextStep && data.steps.some((step) => step.status !== 'completed');
  const dialPercent = Math.max(data.progress_percent, data.progress_percent > 0 ? 4 : 0);

  const showStep = (index: number) => {
    const step = data.steps[index];
    if (!step) return;
    setNavigationDirection(index < currentIndex ? 'backward' : 'forward');
    setSelectedStepId(step.id);
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set('stepId', step.id);
        return next;
      },
      { replace: true }
    );
  };

  const handleSelectStepById = (stepId: string) => {
    const index = data.steps.findIndex((step) => step.id === stepId);
    if (index !== -1) {
      showStep(index);
    }
  };

  const mutationErrorFor = (stepId: string): string | null => {
    if (updateStep.isError && updateStep.variables?.stepId === stepId) {
      return updateStep.error instanceof Error ? updateStep.error.message : 'Не удалось обновить шаг';
    }
    if (updateConfirmation.isError && updateConfirmation.variables?.stepId === stepId) {
      return updateConfirmation.error instanceof Error
        ? updateConfirmation.error.message
        : 'Не удалось обновить подтверждение';
    }
    return null;
  };

  return (
    <div className={styles.container}>
      {plan.isError && (
        <InlineError
          message={plan.error instanceof Error ? plan.error.message : 'Не удалось обновить план'}
          onRetry={() => void plan.refetch()}
          isRetrying={plan.isRefetching}
        />
      )}

      <section className={styles.heroBanner} data-enter>
        <div className={styles.heroInfo}>
          <h1 className={styles.heroTitle}>{data.title}</h1>
          <p className={styles.heroSubtitle}>
            Выполняйте шаги последовательно: отмечайте прогресс здесь, а детали и заявки на
            доступ уточняйте у помощника в чате.
          </p>
        </div>

        <div className={styles.heroProgressGroup}>
          <div className={styles.progressDial}>
            <svg className={styles.progressRing} viewBox="0 0 120 120" aria-hidden="true">
              <circle className={styles.progressRingTrack} cx="60" cy="60" r="54" pathLength="100" />
              <circle
                className={styles.progressRingValue}
                cx="60"
                cy="60"
                r="54"
                pathLength="100"
                strokeDasharray="100"
                strokeDashoffset={100 - dialPercent}
              />
            </svg>
            <span className={styles.progressDialValue}>
              {data.progress_percent}<span className={styles.progressDialMeta}>%</span>
            </span>
          </div>

          <div className={styles.progressContent}>
            <div className={styles.progressLabelRow}>
              <span className={styles.progressLabel}>Текущий прогресс</span>
              <StatusBadge
                status={data.progress_percent === 100 ? 'completed' : 'in_progress'}
                label={`Прогресс: ${data.progress_percent}%`}
                size="sm"
              />
            </div>
            <span className={styles.progressMeta}>
              Завершено {completedCount} из {data.steps.length} шагов.
              {nextStep && ` Следующий шаг: «${nextStep.title}».`}
              {!nextStep && data.progress_percent === 100 && ' План завершён.'}
              {!nextStep && allRemainingBlocked && ' Оставшиеся шаги заблокированы — причина указана в карточке шага.'}
            </span>
            <div
              className={styles.progressBarTrack}
              role="progressbar"
              aria-valuenow={data.progress_percent}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-label="Прогресс плана адаптации"
            >
              <div className={styles.progressBarFill} style={{ width: `${dialPercent}%` }} />
            </div>
          </div>
        </div>
      </section>

      {data.recommendation && (
        <RecommendationCard
          recommendation={data.recommendation}
          onSelectStep={handleSelectStepById}
        />
      )}

      <section className={styles.stepsSection} aria-label="Шаги адаптации">
        <div className={styles.sectionHeader}>
          <h2 className={styles.sectionTitle}>Шаги адаптации</h2>
          <span className={styles.sectionMeta} aria-live="polite">
            {currentStep ? `Шаг ${currentIndex + 1} из ${data.steps.length} · выполнено ${completedCount}` : 'Шагов нет'}
          </span>
        </div>

        {!currentStep ? (
          <EmptyState
            title="Шагов пока нет"
            description="План появится после подготовки данных наставником."
          />
        ) : (
          <>
            <div className={styles.carousel}>
              <button
                type="button"
                className={`${styles.carouselArrow} ${styles.carouselArrowPrevious}`}
                aria-label="Предыдущий шаг"
                aria-controls="current-step"
                disabled={currentIndex <= 0}
                onClick={() => showStep(currentIndex - 1)}
              >
                <ChevronIcon direction="left" />
              </button>

              <div
                id="current-step"
                className={`${styles.stepViewport} ${navigationDirection === 'forward' ? styles.stepForward : styles.stepBackward}`}
                key={currentStep.id}
              >
                <StepCard
                  step={currentStep}
                  index={currentIndex}
                  isNext={currentStep.id === data.next_step_id}
                  isUpdating={updateStep.isPending && updateStep.variables?.stepId === currentStep.id}
                  isCheckpointUpdating={updateConfirmation.isPending}
                  error={mutationErrorFor(currentStep.id)}
                  onStatusChange={(stepId: string, target: StepTarget) =>
                    updateStep.mutate({ stepId, status: target })
                  }
                  onConfirmationChange={(stepId, confirmed, revision) =>
                    updateConfirmation.mutate({ stepId, confirmed, revision })
                  }
                />
              </div>

              <button
                type="button"
                className={`${styles.carouselArrow} ${styles.carouselArrowNext}`}
                aria-label="Следующий шаг"
                aria-controls="current-step"
                disabled={currentIndex >= data.steps.length - 1}
                onClick={() => showStep(currentIndex + 1)}
              >
                <ChevronIcon direction="right" />
              </button>
            </div>

            <div className={styles.stepDots} role="group" aria-label="Навигация по шагам">
              {data.steps.map((step, index) => (
                <button
                  key={step.id}
                  type="button"
                  className={styles.stepDotButton}
                  aria-label={`Шаг ${index + 1}: ${step.title}, ${DOT_STATUS_LABEL[step.status]}`}
                  aria-current={index === currentIndex ? 'step' : undefined}
                  title={`${index + 1}. ${step.title}`}
                  onClick={() => showStep(index)}
                >
                  <span className={`${styles.stepDot} ${DOT_STATUS_CLASS[step.status]}`} aria-hidden="true" />
                </button>
              ))}
            </div>
          </>
        )}
      </section>
    </div>
  );
};
