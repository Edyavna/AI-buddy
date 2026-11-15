-- Synthetic acceptance only: apply after seed in a disposable test database.
BEGIN;
UPDATE onboarding_steps SET status='completed',blocked_reason=NULL
WHERE plan_id IN ('plan_demo_1','plan_demo_2','plan_demo_no_hr');
INSERT INTO onboarding_step_confirmations(step_id,role,user_id)
SELECT s.id,'intern',p.user_id FROM onboarding_steps s JOIN onboarding_plans p ON p.id=s.plan_id
WHERE s.plan_id IN ('plan_demo_1','plan_demo_2','plan_demo_no_hr') AND s.completion_policy IN ('intern','both')
ON CONFLICT DO NOTHING;
INSERT INTO onboarding_step_confirmations(step_id,role,user_id)
SELECT s.id,'mentor',a.mentor_id FROM onboarding_steps s JOIN onboarding_plans p ON p.id=s.plan_id
JOIN mentor_assignments a ON a.intern_id=p.user_id
WHERE s.plan_id IN ('plan_demo_1','plan_demo_2') AND s.completion_policy IN ('mentor','both')
ON CONFLICT DO NOTHING;
INSERT INTO onboarding_steps(id,plan_id,position,title,description,instructions,status,completion_policy,template_key,blocked_reason)
VALUES
('rec55-first','plan_demo_1',501,'Проверка результата наставником','Проверка результата','Ожидайте подтверждения наставника','in_progress','both','rec55-first',NULL),
('rec55-independent','plan_demo_1',502,'Независимая инструкция','Начать работу с документацией','Прочитайте инструкцию и подтвердите выполнение','not_started','intern','rec55-independent',NULL),
('rec55-dependent','plan_demo_1',503,'Зависимый шаг','Результат первого шага','Прочитайте инструкцию после проверки результата','not_started','intern','rec55-dependent',NULL),
('rec55-hr','plan_demo_2',501,'Кадровые документы','Оформление кадровых документов','Уточните порядок оформления','blocked','intern','hr_rec55','Ожидается кадровая инструкция'),
('rec55-no-hr','plan_demo_no_hr',501,'Кадровые документы без HR','Оформление кадровых документов','Уточните порядок оформления','blocked','intern','hr_rec55','Ожидается кадровая инструкция')
ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,blocked_reason=EXCLUDED.blocked_reason;
DELETE FROM onboarding_step_confirmations WHERE step_id IN ('rec55-first','rec55-independent','rec55-dependent');
INSERT INTO onboarding_step_confirmations(step_id,role,user_id) VALUES ('rec55-first','intern','intern_demo_1') ON CONFLICT DO NOTHING;
INSERT INTO onboarding_step_dependencies(plan_id,step_id,prerequisite_id) VALUES ('plan_demo_1','rec55-dependent','rec55-first') ON CONFLICT DO NOTHING;
COMMIT;
