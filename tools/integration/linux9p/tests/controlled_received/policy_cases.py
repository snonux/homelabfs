# SPDX-License-Identifier: GPL-2.0-only
"""Schema/original-binding vectors for the inert policy API, never receipts.

Fields follow guestbridge view_protocol.go/validateViewResponse; mutations
recompute object/request IDs so a semantic rejection cannot hide behind stale
hashes. These vectors carry no signature and cannot enter private Received.
"""
import copy
import hashlib
import json


def canonical(value):
    return json.dumps(value,sort_keys=True,separators=(',',':')).encode()


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def identified(value):
    result=copy.deepcopy(value)
    result.pop('id',None)
    result['id']=digest(result)
    return result


def cases(fixture):
    enrollment=json.loads(bytes.fromhex(fixture['provision']['payload']['groups'][0]['enrollment_bytes']))
    scope=enrollment['Domains'][0]['Scope']
    zero='00'*32
    one='01'*32
    def uid(number):
        return f'{number:08x}-0000-4000-8000-000000000001'
    credentials=dict(credential_revision=1,fsgid=1001,fsuid=1000,groups=[200],idmap_hash=zero,process=uid(60),user_namespace=uid(61))
    ticket=dict(anchor=uid(62),nonce=uid(63),number=1,credentials=credentials)
    cap=dict(ancestors_hash=zero,credential_hash=digest(credentials),description=None,kind='DOMAIN',object_allocation=uid(64),object_home=uid(65),revision=1,rights=0,scope=scope,token=one)
    description=dict(cap,description=uid(66),kind='DESCRIPTION',rights=3)
    target=dict(kind='CONTROLLED_LOCAL',cut=dict(context_hash=digest(scope),enrollment_hash=one,profile_hash=one,journal_epoch=1,journal_hash=one,journal_incarnation=scope['origin_journal'],journal_position=0,state_hash=one,visibility_generation=uid(67)))
    access=dict(kind='DOMAIN')
    lease=identified(dict(access=access,credential_hash=digest(credentials),domain=cap,fence=1,mode='READ',revision=1,scope=scope,target=target))
    acquire=dict(op='VIEW_ACQUIRE',payload=dict(access=access,domain=cap,mode='READ',request=ticket,target=None))
    renew=dict(op='VIEW_RENEW',payload=dict(lease=lease,request=ticket))
    renewed=identified(dict(lease,revision=2))
    number=dict(anchor=uid(68),nonce=uid(69),number=1,realm=uid(70),subject='61')
    dirty_lease=identified(dict(lease,access=dict(kind='PAGE',description=description,offset=0,length=4096),mode='EXCLUSIVE_DIRTY'))
    reservation=identified(dict(claim_number=number,credentials=credentials,description=description,drain_credit=one,lease_id=dirty_lease['id'],length=4096,offset=0,provenance=uid(71),scope=scope))
    reserve=dict(op='DIRTY_RESERVE',payload=dict(claim_number=number,description=description,lease=dirty_lease,length=4096,offset=0,request=ticket))
    data=bytes(range(256))*16
    recall=uid(72)
    entry_core=dict(description=description,length=len(data),offset=0,payload_hash=hashlib.sha256(data).hexdigest(),reservation=reservation['id'])
    entry_core=identified(entry_core)
    intent=dict(opcode='DRAIN_WRITE',args=dict(data=data.hex(),description=description,dirty_entry=entry_core['id'],offset=0,recall=recall,reservation=reservation['id']))
    claim=dict(number,original_hash=digest(intent))
    entry=dict(entry_core,claim=claim)
    freeze=dict(op='DIRTY_FREEZE',payload=dict(claim=claim,data=data.hex(),recall=recall,request=ticket,reservation=reservation))
    def response(original, *, lease=None,reservation=None,entry=None):
        return identified(dict(entry=entry,lease=lease,operation=original['op'],request=ticket,request_hash=digest(original),reservation=reservation,status='FROZEN' if entry else 'GRANTED'))
    bases=[('acquire',acquire,response(acquire,lease=lease)),('renew',renew,response(renew,lease=renewed)),('reserve',reserve,response(reserve,reservation=reservation)),('freeze',freeze,response(freeze,entry=entry))]
    result=[]
    def add(name,original,reply,accept):
        result.append(dict(name=name,scope=scope,original=original,response=reply,accept=accept))
    for name,original,reply in bases:
        add(name,original,reply,True)
    # In every negative, rebind the enclosing request hash/response ID, and
    # recompute nested source IDs when its content changes.
    def response_mutation(name,base,path,value):
        original=copy.deepcopy(base[1]);reply=copy.deepcopy(base[2]);parent=reply
        for key in path[:-1]:parent=parent[key]
        parent[path[-1]]=value
        for key in ['lease','reservation']:
            if reply[key] is not None:reply[key]=identified(reply[key])
        if reply['entry'] is not None:
            entry=reply['entry'];entry['id']=digest({k:entry[k] for k in ['description','length','offset','payload_hash','reservation']})
            # Keep the ORIGINAL drain intent internally consistent even when
            # response manifest fields diverge. Otherwise an unrelated old
            # claim hash could mask a missing original-field comparison.
            request=original['payload'];reserved=request['reservation']
            drain=dict(opcode='DRAIN_WRITE',args=dict(data=request['data'],description=reserved['description'],dirty_entry=entry['id'],offset=reserved['offset'],recall=request['recall'],reservation=reserved['id']))
            request['claim']['original_hash']=digest(drain)
            entry['claim']['original_hash']=digest(drain)
            reply['request_hash']=digest(original)
        reply=identified(reply)
        add(name,original,reply,False)
    for field,value in [('domain',dict(cap,token=zero)),('access',dict(kind='OBJECT',object=cap)),('mode','EXCLUSIVE_DIRTY'),('target',dict(target,cut=dict(target['cut'],state_hash=zero)))]:
        if field=='target':
            original=copy.deepcopy(acquire);original['payload']['target']=target
            base=('acquire-target',original,response(original,lease=lease))
        else:base=bases[0]
        response_mutation('acquire-'+field,base,['lease',field],value)
    for field,value in [('domain',dict(cap,token=zero)),('access',dict(kind='OBJECT',object=cap)),('credential_hash',zero),('mode','EXCLUSIVE_DIRTY'),('target',dict(target,cut=dict(target['cut'],state_hash=zero))),('fence',2),('revision',3),('scope',dict(scope,session=uid(90)))]:
        response_mutation('renew-'+field,bases[1],['lease',field],value)
    for field,value in [('description',dict(description,token=zero)),('offset',4096),('length',2048),('claim_number',dict(number,number=2)),('lease_id',zero),('credentials',dict(credentials,fsuid=1002))]:
        response_mutation('reserve-'+field,bases[2],['reservation',field],value)
    for field,value in [('description',dict(description,token=zero)),('offset',4096),('length',4095),('payload_hash',zero),('reservation',zero),('claim',dict(claim,number=2))]:
        response_mutation('freeze-'+field,bases[3],['entry',field],value)
    for label,mutate in [('data',lambda p:p.update(data='00'*4096)),('recall',lambda p:p.update(recall=uid(73))),('empty',lambda p:p.update(data='')),('over-page',lambda p:p.update(data='00'*4097)),('claim-hash',lambda p:p['claim'].update(original_hash=zero)),('reservation-number',lambda p:p['reservation']['claim_number'].update(number=2))]:
        original=copy.deepcopy(freeze);mutate(original['payload']);reply=copy.deepcopy(bases[3][2]);reply['request_hash']=digest(original);reply=identified(reply);add('freeze-original-'+label,original,reply,False)
    for label,key in [('unknown-entry','future'),('missing-entry','length'),('unknown-description','future'),('unknown-claim','future')]:
        reply=copy.deepcopy(bases[3][2]);entry=reply['entry']
        parent=entry['description'] if label=='unknown-description' else entry['claim'] if label=='unknown-claim' else entry
        if label.startswith('missing'):del parent[key]
        else:parent[key]=0
        original=copy.deepcopy(freeze)
        if label=='unknown-description':
            original['payload']['reservation']['description']=copy.deepcopy(entry['description'])
            original['payload']['reservation']=identified(original['payload']['reservation'])
            entry['reservation']=original['payload']['reservation']['id']
            entry['id']=digest({k:entry[k] for k in ['description','length','offset','payload_hash','reservation']})
            request=original['payload'];reserved=request['reservation']
            drain=dict(opcode='DRAIN_WRITE',args=dict(data=request['data'],description=reserved['description'],dirty_entry=entry['id'],offset=reserved['offset'],recall=request['recall'],reservation=reserved['id']))
            original['payload']['claim']['original_hash']=digest(drain)
            entry['claim']=copy.deepcopy(original['payload']['claim'])
        if label=='unknown-claim':original['payload']['claim']=copy.deepcopy(entry['claim'])
        # Rebind every containing hash and corresponding original field. These
        # extra-key negatives isolate nested closure rather than stale hashes.
        reply['request_hash']=digest(original)
        reply=identified(reply);add('freeze-schema-'+label,original,reply,False)
    return result
