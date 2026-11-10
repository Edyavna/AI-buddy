import React from 'react';
import { EmptyState, InlineError, Spinner, StatusBadge } from '@/shared/ui';
import { usePlan, useUpdateStep } from './hooks';
import { StepCard } from './StepCard';
import type { StepTarget } from './schemas';
import styles from './PlanPage.module.css';

export const PlanPage: React.FC = () => {
  const plan = usePlan();
  const updateStep = useUpdateStep();

  if (plan.isPending) {
    return (
      <div className={styles.loadingState} role="status" aria-label="Загрузка плана адаптации">
        <Spinner size="lg" />
      </div>
    );
  }

  if (plan.isError) {
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
  const allRemainingBlocked =
    !nextStep && data.steps.some((step) => step.status !== 'completed');
  const dialPercent = Math.max(data.progress_percent, data.progress_percent > 0 ? 4 : 0);

  const mutationErrorFor = (stepId: string): string | null => {
    if (!updateStep.isError || !updateStep.variables) return null;
    return updateStep.variables.stepId === stepId
      ? updateStep.error instanceof Error
        ? updateStep.error.message
        : 'Не удалось обновить шаг'
      : null;
  };

  return (
    <div className={styles.container}>
      <section className={styles.heroBanner} data-enter>
        <div className={styles.heroInfo}>
          <h1 className={styles.heroTitle}>{data.title}</h1>
          <p className={styles.heroSubtitle}>
            Выполняйте шаги последовательно: отмечайте прогресс здесь, а детали и заявки на
            доступ уточняйте у помощника в чате.
          </p>
        </div>

        <div className={styles.heroProgressGroup}>
          <div
            className={styles.progressDial}
            style={{ background: `conic-gradient(var(--avito-blue) 0 ${dialPercent}%, rgba(0, 170, 255, 0.16) ${dialPercent}% 100%)` }}
            aria-hidden="true"
          >
            <span className={styles.progressDialValue}>{data.progress_percent}</span>
            <span className={styles.progressDialMeta}>%</span>
          </div>
          <div className={styles.progressLabelRow}>
            <span className={styles.progressLabel}>Текущий прогресс</span>
            <StatusBadge
              status={data.progress_percent === 100 ? 'completed' : 'in_progress'}
              label={`Прогресс: ${data.progress_percent}%`}
              size="sm"
            />
          </div>
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
          <span className={styles.progressMeta}>
            Завершено {completedCount} из {data.steps.length} шагов.
            {nextStep && ` Следующий шаг: «${nextStep.title}».`}
            {!nextStep && data.progress_percent === 100 && ' План завершён.'}
            {!nextStep && allRemainingBlocked && ' Оставшиеся шаги заблокированы — причина указана в карточке шага.'}
          </span>
        </div>
      </section>

      <section className={styles.stepsSection} aria-label="Шаги адаптации">
        <div className={styles.sectionHeader}>
          <h2 className={styles.sectionTitle}>Шаги адаптации</h2>
          <span className={styles.progressMeta}>
            {data.steps.length === 0 ? 'Шагов нет' : `${completedCount} из ${data.steps.length} выполнено`}
          </span>
        </div>

        {data.steps.length === 0 ? (
          <EmptyState
            title="Шагов пока нет"
            description="План появится после подготовки данных наставником."
          />
        ) : (
          <div className={styles.stepsList}>
            {data.steps.map((step, index) => (
              <StepCard
                key={step.id}
                step={step}
                index={index}
                isNext={step.id === data.next_step_id}
                isUpdating={updateStep.isPending && updateStep.variables?.stepId === step.id}
                error={mutationErrorFor(step.id)}
                onStatusChange={(stepId: string, target: StepTarget) =>
                  updateStep.mutate({ stepId, status: target })
                }
              />
            ))}
          </div>
        )}
      </section>
    </div>
  );
};
