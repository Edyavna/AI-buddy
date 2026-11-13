"""Validate API structure, fixtures, cross-object links and Markdown references."""
from pathlib import Path
import copy
import json
import re
import yaml
from jsonschema import FormatChecker, ValidationError
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT4
from openapi_schema_validator import OAS30Validator
from openapi_spec_validator import validate

ROOT = Path(__file__).resolve().parents[1]
spec = yaml.safe_load((ROOT / 'contracts/openapi.yaml').read_text(encoding='utf-8'))
validate(spec)
registry = Registry().with_resource('urn:ai-buddy:openapi', Resource.from_contents(spec, default_specification=DRAFT4))
def check(schema, value):
    OAS30Validator({'$ref': 'urn:ai-buddy:openapi#/components/schemas/' + schema}, registry=registry,
                   format_checker=FormatChecker()).validate(value)

f = json.loads((ROOT / 'contracts/examples/scenario.json').read_text(encoding='utf-8'))
mapping = {'user':'User', 'create_conversation':'CreateConversation', 'conversation':'Conversation',
           'send_message':'SendMessage', 'confirm':'ConfirmRequest', 'error':'ApiError',
           'failed_event':'ChatEvent'}
mapping.update({k:'AccessRequest' for k in ['draft','submitting','pending','granted']})
mapping.update({k:'Plan' for k in ['plan','blocked_plan','granted_plan','completed_plan']})
for key,schema in mapping.items(): check(schema,f[key])
for message in f['messages']: check('Message',message)
for event in f['events']: check('ChatEvent',event)

assignment = json.loads((ROOT / 'contracts/examples/mentor-assignment.json').read_text(encoding='utf-8'))
check('AvailableInternList', assignment['available'])
check('AvailableInternList', assignment['empty'])
check('MentorAssignment', assignment['assigned'])
check('ApiError', assignment['conflict'])
assert assignment['assigned']['intern_id'] == assignment['available']['items'][0]['id']
assert assignment['assigned']['team_id'] == assignment['available']['items'][0]['team']['id']
assert assignment['conflict']['error']['code'] == 'INTERN_ALREADY_ASSIGNED'
assert 'requestBody' not in spec['paths']['/mentor/interns/{id}/assignment']['put']

hr = json.loads((ROOT / 'contracts/examples/hr.json').read_text(encoding='utf-8'))
for user in hr['users']:
    check('User', user)
for scope in hr['scopes'].values():
    check('AccessScope', scope)
for key, schema in [('routing', 'RoutingSuggestion'), ('question', 'RoutedQuestion'),
                    ('no_hr', 'RoutedQuestions'), ('document', 'HRDocument'), ('foreign', 'ApiError')]:
    check(schema, hr[key])
assert hr['question']['team_id'] in {t['id'] for t in hr['scopes']['hr_demo_1']['teams']}
assert hr['question']['team_id'] in {t['id'] for t in hr['scopes']['hr_demo_2']['teams']}
assert hr['no_hr']['available_directions'] == []

checkpoints = json.loads((ROOT / 'contracts/examples/checkpoints.json').read_text(encoding='utf-8'))
for key in ['confirm', 'revoke']:
    check('SetCheckpointConfirmation', checkpoints[key])
for plan in checkpoints['plans'].values():
    check('Plan', plan)
    step = plan['steps'][0]
    marks = {mark['role'] for mark in step['confirmations']}
    assert len(marks) == len(step['confirmations'])
    required = {'intern', 'mentor'} if step['completion_policy'] == 'both' else {step['completion_policy']}
    assert marks <= required
    if step['status'] != 'blocked':
        assert (step['status'] == 'completed') == (marks == required)
    assert step['awaiting_role'] == (next(iter(required - marks)) if marks and marks < required else None)
    assert plan['progress_percent'] == (100 if step['status'] == 'completed' else 0)
for error in checkpoints['errors'].values():
    check('ApiError', error)
assert checkpoints['errors']['revision_conflict']['error']['code'] == 'CHECKPOINT_REVISION_CONFLICT'

full_demo = json.loads((ROOT / 'contracts/examples/full-demo-plan.json').read_text(encoding='utf-8'))
check('Plan', full_demo)
demo_steps = full_demo['steps']
assert len(demo_steps) == len({s['template_key'] for s in demo_steps}) == 16
assert len({s['title'] for s in demo_steps}) == 14
assert {s['completion_policy'] for s in demo_steps} == {'intern', 'mentor', 'both'}
assert any(r['url'] is None and r['note'] for s in demo_steps for r in s['resources'])

assert f['messages'][1]['in_reply_to_message_id'] == f['messages'][0]['id']
assert f['messages'][0]['client_message_id'] == f['send_message']['client_message_id']
assert f['conversation']['client_conversation_id'] == f['create_conversation']['client_conversation_id']
assert f['draft']['id'] in f['messages'][1]['request_ids']
assert f['draft']['step_id'] == f['conversation']['step_id'] == f['plan']['steps'][0]['id']
assert f['draft']['conversation_id'] == f['conversation']['id']
assert f['confirm']['draft_version'] == f['draft']['draft_version']
assert f['events'][0]['event'] == 'message.started'
assert f['events'][-1]['event'] == 'message.completed'
assert sum(e['event'] in ('message.completed','message.failed') for e in f['events']) == 1
assert ''.join(e['data']['text'] for e in f['events'] if e['event']=='answer.delta') == f['messages'][1]['text']
assert f['failed_event']['data']['message']['status']=='failed'
assert 'error' in f['failed_event']['data']['message']
for key in ['plan','blocked_plan','granted_plan','completed_plan']:
    p=f[key]; steps=p['steps']
    assert p['progress_percent'] == (100*sum(x['status']=='completed' for x in steps)//len(steps) if steps else 0)
    next_id=next((x['id'] for x in steps if x['status'] not in ('completed','blocked')),None)
    assert p['next_step_id']==next_id
    for step in steps:
        assert bool(step['blocked_reason']) == (step['status']=='blocked')
revisions=[]
for key in ['draft','submitting','pending','granted']:
    request=f[key]; revisions.append(request['revision'])
    if key in ('pending','granted'): assert request['external_id']
assert revisions==sorted(set(revisions))

# Negative cases prove the schemas reject missing correlation/version/invalid payloads.
for schema,value,field in [('AccessRequest',f['draft'],'revision'),('Message',f['messages'][1],'in_reply_to_message_id'),('CreateConversation',f['create_conversation'],'client_conversation_id')]:
    bad=copy.deepcopy(value); del bad[field]
    try: check(schema,bad)
    except ValidationError: pass
    else: raise AssertionError('Missing field accepted: '+field)
bad=copy.deepcopy(f['send_message']);bad['text']='   '
try: check('SendMessage',bad)
except ValidationError: pass
else: raise AssertionError('Whitespace-only text accepted')
for schema,bad in [
    ('SendMessage', {**f['send_message'], 'user_id':'forged_user'}),
    ('AccessRequest', {**f['draft'], 'status':'approved'}),
    ('SetCheckpointConfirmation', {'confirmed': True, 'revision': 0}),
    ('SetCheckpointConfirmation', {'confirmed': False}),
    ('SetCheckpointConfirmation', {**checkpoints['confirm'], 'mentor_id': 'forged'}),
    ('ChatEvent', {'event':'answer.delta','data':{'message_id':'msg_a1','text':123}}),
]:
    try: check(schema,bad)
    except ValidationError: pass
    else: raise AssertionError('Invalid payload accepted: '+schema)

ids=[]
for path,item in spec['paths'].items():
    for method,op in item.items():
        if method not in ('get','post','put','patch','delete'): continue
        ids.append(op['operationId'])
        assert 'default' in op['responses'],path
assert len(ids)==len(set(ids))
for p in [ROOT/'README.md',*list((ROOT/'contracts').glob('*.md')),*list((ROOT/'docs/frontend').glob('*.md'))]:
    text=p.read_text(encoding='utf-8')
    assert text.count('```')%2==0,p
    for target in re.findall(r'\]\(([^)]+)\)',text):
        if '://' in target or target.startswith('#'): continue
        assert (p.parent/target.split('#')[0]).exists(),(p,target)
print(f'PASS: OpenAPI {spec["info"]["version"]}; {len(ids)} operations; fixtures, SSE schemas, negative cases, references')
