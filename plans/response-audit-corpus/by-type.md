# Response corpus — by response-type (component & size breakdown)

Source: 428 transcripts, 5298 ZCP responses, 93 response-kinds. Recent = date >= 20260604 (post-consolidation), errors excluded from size stats.

Tools: `zerops_workflow`(2724), `zerops_discover`(704), `zerops_deploy`(474), `zerops_verify`(437), `zerops_import`(280), `zerops_dev_server`(233), `zerops_knowledge`(183), `zerops_env`(58), `zerops_events`(53), `zerops_browser`(43), `zerops_logs`(38), `zerops_recipe`(23), `zerops_subdomain`(21), `zerops_mount`(10), `zerops_export`(7), `zerops_scale`(6), `zerops_manage`(4)


## `workflow:develop:start::prose`
- count: 265 total / 47 recent
- bytes: p50=15599 p90=21775 max=23035 min=9407
- avg component bytes (top): _text=16520
- seen in: api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, cadence-multiservice-spec-run2-replay, classic-bun-simple
- sample: samples/workflow-develop-start-prose.json

## `workflow:complete:discover::session-active`
- count: 343 total / 77 recent
- bytes: p50=5078 p90=7699 max=8402 min=3369
- avg component bytes (top): current=4980, progress=171, intent=125, message=36, sessionId=18, kind=16
- seen in: adopt-existing-standard-pair, api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, cadence-multiservice-spec-run2-replay
- sample: samples/workflow-complete-discover-session-active.json

## `discover`
- count: 704 total / 116 recent
- bytes: p50=1347 p90=4969 max=11958 min=257
- avg component bytes (top): services=2079, warnings=513, project=194, notes=145
- seen in: adopt-existing-standard-pair, api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, cadence-multiservice-spec-run2-replay
- sample: samples/discover.json

## `workflow:complete:provision::session-active`
- count: 347 total / 77 recent
- bytes: p50=3405 p90=5579 max=7531 min=881
- avg component bytes (top): current=4326, checkResult=257, message=198, progress=170, intent=125, autoMounts=82, sessionId=18, kind=16
- seen in: adopt-existing-standard-pair, api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, cadence-multiservice-spec-run2-replay
- sample: samples/workflow-complete-provision-session-active.json

## `knowledge`
- count: 183 total / 22 recent
- bytes: p50=5618 p90=36584 max=36584 min=1121
- avg component bytes (top): _text=12574
- seen in: classic-php-mariadb-standard, develop-add-managed-dep-to-existing, export-buildfromgit-self-snapshot, greenfield-fullstack-multi-runtime
- sample: samples/knowledge.json

## `workflow:bootstrap/classic:start::session-active`
- count: 154 total / 25 recent
- bytes: p50=7693 p90=7877 max=7932 min=7651
- avg component bytes (top): current=6086, availableStacks=1214, progress=170, intent=150, message=20, sessionId=18, kind=16
- seen in: cadence-multiservice-build-run2-replay, classic-bun-simple, classic-go-simple, classic-php-mariadb-standard
- sample: samples/workflow-bootstrap-classic-start-session-active.json

## `workflow:bootstrap:start::route-menu`
- count: 265 total / 54 recent
- bytes: p50=3867 p90=6014 max=8332 min=732
- avg component bytes (top): routeOptions=2240, routeOptions.importYaml(sum)=1563, message=943, routeOptions.why(sum)=443, intent=150, projectId=24, kind=12
- seen in: adopt-existing-standard-pair, api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, cadence-multiservice-spec-run2-replay
- sample: samples/workflow-bootstrap-start-route-menu.json

## `workflow:bootstrap/adopt:start::session-active`
- count: 128 total / 34 recent
- bytes: p50=5261 p90=5298 max=5366 min=5170
- avg component bytes (top): current=3652, availableStacks=1214, progress=170, intent=91, message=20, sessionId=18, kind=16
- seen in: adopt-existing-standard-pair, cross-deploy-stage-promote-from-dev, discover-adoption-state-resumable-uses-sessionid, existing-standard-appdev-only-reminders
- sample: samples/workflow-bootstrap-adopt-start-session-active.json

## `workflow:bootstrap/recipe:start::session-active`
- count: 63 total / 18 recent
- bytes: p50=7059 p90=8754 max=9297 min=6330
- avg component bytes (top): current=5635, availableStacks=1214, progress=170, intent=157, message=20, sessionId=18, kind=16
- seen in: api-node-postgres-classic-dev, cadence-multiservice-spec-run2-replay, classic-python-postgres-dev-only, classic-rust-postgres-standard
- sample: samples/workflow-bootstrap-recipe-start-session-active.json

## `workflow:launch-production:start::status=source-control-required`
- count: 64 total / 19 recent
- bytes: p50=6583 p90=6681 max=6685 min=5886
- avg component bytes (top): guidance=4898, blockers=760, sourceContext=233, inputs=55, phase=26, status=25, workflow=19
- seen in: launch-production-dev-only, launch-production-existing-project-token, launch-production-existing-with-webhook, launch-production-from-standard-pair
- sample: samples/workflow-launch-production-start-status-source-control-requi.json

## `verify`
- count: 437 total / 86 recent
- bytes: p50=1138 p90=2259 max=6628 min=599
- avg component bytes (top): services=3504, checks=680, workSessionState=333, note=269, runtimeClassification=65, typeVersion=19, summary=13, type=9, status=9, runtimeClass=8
- seen in: api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, classic-bun-simple, classic-go-simple
- sample: samples/verify.json

## `workflow:export:start::status=classify-prompt`
- count: 3 total / 3 recent
- bytes: p50=23768 p90=23768 max=23768 min=23768
- avg component bytes (top): guidance=18282, zeropsYaml=4208, envClassificationTable=518, warnings=309, nextSteps=120, fetchValuesVia=75, status=17, phase=15, targetService=8
- seen in: export-buildfromgit-self-snapshot
- sample: samples/workflow-export-start-status-classify-prompt.json

## `browser`
- count: 43 total / 17 recent
- bytes: p50=2154 p90=4087 max=29828 min=678
- avg component bytes (top): steps=4063, consoleOutput=225, errorsOutput=92, message=48, url=39, durationMs=4
- seen in: cadence-multiservice-build-run2-replay, kanban-laravel-minimal-dev-only, pm-app-czech-byty-run1-replay, verify-subdomain-recovery-before-browser
- sample: samples/browser.json

## `deploy`
- count: 474 total / 66 recent
- bytes: p50=780 p90=964 max=5768 min=556
- avg component bytes (top): buildLogs=2409, runtimeLogs=1817, failureClassification=559, suggestion=353, workSessionState=287, nextActions=96, monitorHint=81, message=78, warnings=59, subdomainUrl=40
- seen in: api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, classic-bun-simple, classic-go-simple
- sample: samples/deploy.json

## `workflow:?:status::prose`
- count: 41 total / 9 recent
- bytes: p50=7112 p90=7112 max=7112 min=4485
- avg component bytes (top): _text=6528
- seen in: cross-deploy-stage-promote-from-dev, discover-adoption-state-resumable-uses-sessionid, launch-production-existing-with-webhook, launch-production-pipeline-configured
- sample: samples/workflow-status-prose.json

## `workflow:complete:close::session-active`
- count: 214 total / 43 recent
- bytes: p50=1136 p90=1344 max=1385 min=940
- avg component bytes (top): message=727, progress=169, intent=153, sessionId=18, kind=16
- seen in: api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, cadence-multiservice-spec-run2-replay, classic-bun-simple
- sample: samples/workflow-complete-close-session-active.json

## `workflow:?:git-push-setup::status=walkthrough`
- count: 37 total / 6 recent
- bytes: p50=7001 p90=7001 max=7001 min=6974
- avg component bytes (top): guidance=4285, inputsRequired=1156, steps=468, nextStep=381, prompt=288, workSessionState=215, status=13, recommendedIntegration=9, service=7
- seen in: delivery-git-push-actions-setup, git-push-setup-then-actions, git-push-setup-with-cicd-method-prompt, launch-production-pipeline-not-configured
- sample: samples/workflow-git-push-setup-status-walkthrough.json

## `workflow:export:start::status=validation-failed`
- count: 3 total / 3 recent
- bytes: p50=13902 p90=13902 max=13902 min=13681
- avg component bytes (top): guidance=8306, preview=5079, nextSteps=163, errors=96, status=19, phase=15, targetService=8
- seen in: export-buildfromgit-self-snapshot
- sample: samples/workflow-export-start-status-validation-failed.json

## `import`
- count: 280 total / 45 recent
- bytes: p50=825 p90=1283 max=2008 min=394
- avg component bytes (top): processes=693, warnings=315, nextActions=109, summary=40, projectId=24, projectName=10
- seen in: api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, cadence-multiservice-spec-run2-replay, classic-bun-simple
- sample: samples/import.json

## `workflow:launch-production:start::status=scope-prompt`
- count: 44 total / 5 recent
- bytes: p50=4612 p90=4670 max=4670 min=4612
- avg component bytes (top): guidance=4062, sourceContext=226, blockers=183, phase=26, workflow=19, status=14, inputs=2
- seen in: launch-production-existing-project-token, launch-production-existing-with-webhook, launch-production-from-standard-pair, launch-production-pipeline-skip
- sample: samples/workflow-launch-production-start-status-scope-prompt.json

## `dev_server:start`
- count: 184 total / 27 recent
- bytes: p50=365 p90=688 max=4541 min=248
- avg component bytes (top): logTail=381, message=106, logFile=25, reason=22, action=7, hostname=7, healthPath=5, running=4, port=4, healthStatus=3
- seen in: api-node-postgres-classic-dev, classic-python-postgres-dev-only, classic-rust-postgres-standard, cross-deploy-stage-promote-from-dev
- sample: samples/dev-server-start.json

## `workflow:export:start::status=compose-ready`
- count: 2 total / 2 recent
- bytes: p50=7667 p90=7679 max=7679 min=7667
- avg component bytes (top): bundle=5058, guidance=2212, nextSteps=269, phase=15, status=15, targetService=8
- seen in: export-buildfromgit-self-snapshot
- sample: samples/workflow-export-start-status-compose-ready.json

## `workflow:?:status::session-active`
- count: 10 total / 3 recent
- bytes: p50=2979 p90=9297 max=9297 min=2979
- avg component bytes (top): current=4312, availableStacks=1214, progress=168, intent=80, sessionId=30, message=20, kind=16
- seen in: discover-adoption-state-resumable-uses-sessionid, recipe-laravel-showcase-fullstack
- sample: samples/workflow-status-session-active.json

## `workflow:?:close-mode::status=updated`
- count: 236 total / 43 recent
- bytes: p50=420 p90=466 max=540 min=178
- avg component bytes (top): workSessionState=270, nextSteps=202, services=18, status=9
- seen in: api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, classic-bun-simple, classic-go-simple
- sample: samples/workflow-close-mode-status-updated.json

## `workflow:export:start::status=git-push-setup-required`
- count: 1 total / 1 recent
- bytes: p50=11910 p90=11910 max=11910 min=11910
- avg component bytes (top): guidance=6735, preview=4786, nextSteps=172, reason=28, status=25, phase=15, targetService=8
- seen in: export-buildfromgit-self-snapshot
- sample: samples/workflow-export-start-status-git-push-setup-required.json

## `workflow:launch-production:start::status=classify-prompt`
- count: 38 total / 1 recent
- bytes: p50=8540 p90=8540 max=8540 min=8540
- avg component bytes (top): guidance=7595, classifications=518, sourceContext=264, phase=26, workflow=19, status=17
- seen in: launch-with-existing-cicd
- sample: samples/workflow-launch-production-start-status-classify-prompt.json

## `workflow:?:build-integration::status=configured`
- count: 22 total / 2 recent
- bytes: p50=4143 p90=4143 max=4143 min=4143
- avg component bytes (top): workflowFile=827, ghAuthPrecondition=688, secrets=648, alternateWorkflowFiles=569, nextStep=367, ghPatRecommendation=301, topologyNote=275, workSessionState=215, status=12, buildTarget=10
- seen in: git-push-setup-then-actions, launch-with-existing-cicd
- sample: samples/workflow-build-integration-status-configured.json

## `workflow:launch-production:status::prose`
- count: 11 total / 1 recent
- bytes: p50=7112 p90=7112 max=7112 min=7112
- avg component bytes (top): _text=7112
- seen in: launch-production-pipeline-not-configured
- sample: samples/workflow-launch-production-status-prose.json

## `workflow:bootstrap/resume:start::session-active`
- count: 4 total / 2 recent
- bytes: p50=2979 p90=2979 max=2979 min=2979
- avg component bytes (top): current=2697, progress=167, sessionId=37, message=21, kind=16, intent=2
- seen in: discover-adoption-state-resumable-uses-sessionid
- sample: samples/workflow-bootstrap-resume-start-session-active.json

## `events`
- count: 53 total / 5 recent
- bytes: p50=1047 p90=2540 max=2540 min=313
- avg component bytes (top): events=1152, summary=42, projectId=24
- seen in: recover-failed-buildfromgit-missing-dep, resume-after-compaction, verify-subdomain-recovery-before-browser
- sample: samples/events.json

## `workflow:launch-production:start::status=launched`
- count: 6 total / 1 recent
- bytes: p50=5603 p90=5603 max=5603 min=5603
- avg component bytes (top): guidance=4879, blockers=363, warnings=98, inputs=42, phase=26, productionProjectId=24, workflow=19, status=10
- seen in: launch-with-existing-cicd
- sample: samples/workflow-launch-production-start-status-launched.json

## `workflow:bootstrap:status::prose`
- count: 1 total / 1 recent
- bytes: p50=5453 p90=5453 max=5453 min=5453
- avg component bytes (top): _text=5453
- seen in: adopt-existing-standard-pair
- sample: samples/workflow-bootstrap-status-prose.json

## `env:get`
- count: 11 total / 3 recent
- bytes: p50=1888 p90=1902 max=1902 min=817
- avg component bytes (top): envs=1531, service=114, project=72
- seen in: git-push-setup-with-cicd-method-prompt, kanban-laravel-minimal-standard-pair
- sample: samples/env-get.json

## `env:set`
- count: 47 total / 9 recent
- bytes: p50=537 p90=538 max=538 min=419
- avg component bytes (top): process=212, nextActions=167, stored=83, restartSkipped=4
- seen in: kanban-laravel-minimal-dev-only, kanban-laravel-minimal-standard-pair, recipe-laravel-minimal-standard, recipe-laravel-showcase-fullstack
- sample: samples/env-set.json

## `logs`
- count: 38 total / 4 recent
- bytes: p50=30 p90=3983 max=3983 min=30
- avg component bytes (top): entries=1027, hasMore=5
- seen in: pm-app-czech-byty-run1-replay, recover-failed-buildfromgit-missing-dep, verify-subdomain-recovery-before-browser
- sample: samples/logs.json

## `workflow:?:git-push-setup::status=configured`
- count: 27 total / 3 recent
- bytes: p50=1006 p90=1006 max=1006 min=1006
- avg component bytes (top): nextStep=609, workSessionState=215, remoteUrl=35, gitPushState=12, status=12, recommendedIntegration=9, service=8
- seen in: git-push-setup-then-actions, launch-with-existing-cicd
- sample: samples/workflow-git-push-setup-status-configured.json

## `workflow:?:build-integration::status=needsGitPushSetup`
- count: 11 total / 5 recent
- bytes: p50=508 p90=512 max=512 min=502
- avg component bytes (top): workSessionState=215, nextStep=113, reason=91, status=19, service=7
- seen in: delivery-git-push-actions-setup, git-push-setup-with-cicd-method-prompt, launch-production-pipeline-not-configured
- sample: samples/workflow-build-integration-status-needsgitpushsetup.json

## `workflow:launch-production:start::status=ready-to-launch`
- count: 19 total / 1 recent
- bytes: p50=2310 p90=2310 max=2310 min=2310
- avg component bytes (top): guidance=1878, sourceContext=264, inputs=42, phase=26, workflow=19, status=17
- seen in: launch-with-existing-cicd
- sample: samples/workflow-launch-production-start-status-ready-to-launch.json

## `workflow:?:iterate::session-active`
- count: 3 total / 2 recent
- bytes: p50=1144 p90=1144 max=1144 min=1144
- avg component bytes (top): current=862, progress=167, sessionId=37, message=21, kind=16, intent=2
- seen in: discover-adoption-state-resumable-uses-sessionid
- sample: samples/workflow-iterate-session-active.json

## `dev_server:restart`
- count: 29 total / 4 recent
- bytes: p50=367 p90=564 max=564 min=306
- avg component bytes (top): logTail=123, message=87, logFile=25, action=9, hostname=7, healthPath=6, running=4, port=4, startMillis=4, healthStatus=3
- seen in: develop-loop-after-bootstrap, env-cross-runtime-ref-lifecycle, existing-standard-appdev-only-reminders, greenfield-fullstack-multi-runtime
- sample: samples/dev-server-restart.json

## `subdomain:enable`
- count: 21 total / 1 recent
- bytes: p50=674 p90=674 max=674 min=674
- avg component bytes (top): process=288, warnings=183, nextActions=42, subdomainUrls=39, serviceId=24, serviceHostname=8, action=8
- seen in: verify-subdomain-recovery-before-browser
- sample: samples/subdomain-enable.json

## `workflow:?:record-deploy::obj`
- count: 2 total / 1 recent
- bytes: p50=571 p90=571 max=571 min=571
- avg component bytes (top): workSessionState=299, note=99, subdomainUrl=39, firstDeployedAt=22, hostname=5, stamped=4, subdomainAccessEnabled=4
- seen in: recipe-nextjs-ssr-frontend-standard
- sample: samples/workflow-record-deploy-obj.json

## `scale`
- count: 6 total / 1 recent
- bytes: p50=393 p90=393 max=393 min=393
- avg component bytes (top): process=284, nextActions=34, serviceId=24, serviceHostname=8
- seen in: greenfield-fullstack-multi-runtime
- sample: samples/scale.json

## `workflow:?:reset::obj`
- count: 6 total / 1 recent
- bytes: p50=152 p90=152 max=152 min=152
- avg component bytes (top): cleared=113, preserved=20
- seen in: discover-adoption-state-resumable-uses-sessionid
- sample: samples/workflow-reset-obj.json

## `workflow:bootstrap:reset::obj`
- count: 2 total / 1 recent
- bytes: p50=152 p90=152 max=152 min=152
- avg component bytes (top): cleared=113, preserved=20
- seen in: discover-adoption-state-resumable-uses-sessionid
- sample: samples/workflow-bootstrap-reset-obj.json

## `export`
- count: 7 total / 1 recent
- bytes: p50=145 p90=145 max=145 min=145
- avg component bytes (top): _text=145
- seen in: verify-subdomain-recovery-before-browser
- sample: samples/export.json

## `workflow:?:close::prose`
- count: 2 total / 2 recent
- bytes: p50=20 p90=20 max=20 min=20
- avg component bytes (top): _text=20
- seen in: cross-deploy-stage-promote-from-dev, export-buildfromgit-self-snapshot
- sample: samples/workflow-close-prose.json

## `workflow:develop:close::prose`
- count: 5 total / 1 recent
- bytes: p50=20 p90=20 max=20 min=20
- avg component bytes (top): _text=20
- seen in: verify-subdomain-recovery-before-browser
- sample: samples/workflow-develop-close-prose.json

## `workflow:?:list::nondict`
- count: 2 total / 1 recent
- bytes: p50=2 p90=2 max=2 min=2
- seen in: launch-production-pipeline-configured
- sample: samples/workflow-list-nondict.json

## `workflow:bootstrap:start::obj`
- count: 20 total / 0 recent
- bytes: p50=3157 p90=7208 max=10417 min=553
- avg component bytes (top): routeOptions=2997, routeOptions.importYaml(sum)=2420, message=762, routeOptions.why(sum)=411, intent=78, projectId=24
- seen in: classic-go-simple, classic-php-mariadb-standard, classic-python-postgres-dev-only, classic-static-nginx-simple
- sample: samples/workflow-bootstrap-start-obj.json

## `workflow:bootstrap/classic:start::error`
- count: 8 total / 0 recent
- bytes: p50=356 p90=356 max=356 min=356
- avg component bytes (top): suggestion=147, error=102, recovery=47, code=19
- seen in: classic-go-simple, develop-add-managed-dep-to-existing, env-cross-runtime-ref-lifecycle, greenfield-fullstack-multi-runtime
- sample: samples/workflow-bootstrap-classic-start-error.json

## `workflow:bootstrap/classic:start::obj`
- count: 17 total / 0 recent
- bytes: p50=7530 p90=7576 max=7606 min=7447
- avg component bytes (top): current=6453, availableStacks=729, progress=170, intent=50, message=20, sessionId=18
- seen in: classic-go-simple, classic-php-mariadb-standard, classic-python-postgres-dev-only, classic-static-nginx-simple
- sample: samples/workflow-bootstrap-classic-start-obj.json

## `workflow:?:complete::obj`
- count: 52 total / 0 recent
- bytes: p50=6226 p90=7158 max=7262 min=1594
- avg component bytes (top): current=5880, availableStacks=729, message=230, checkResult=199, progress=170, autoMounts=76, intent=43, sessionId=18
- seen in: classic-go-simple, classic-php-mariadb-standard, classic-python-postgres-dev-only, classic-static-nginx-simple
- sample: samples/workflow-complete-obj.json

## `dev_server:stop`
- count: 9 total / 0 recent
- bytes: p50=162 p90=162 max=164 min=156
- avg component bytes (top): message=85, hostname=7, action=6, running=5, port=4
- seen in: cadence-multiservice-build-run2-replay, cadence-multiservice-spec-run2-replay, classic-go-simple, greenfield-fullstack-multi-runtime
- sample: samples/dev-server-stop.json

## `workflow:?:complete::error`
- count: 64 total / 0 recent
- bytes: p50=1307 p90=1348 max=1861 min=190
- avg component bytes (top): error=639, suggestion=114, recovery=47, code=19
- seen in: adopt-existing-standard-pair, api-node-postgres-classic-dev, cadence-multiservice-build-run2-replay, classic-php-mariadb-standard
- sample: samples/workflow-complete-error.json

## `workflow:bootstrap/recipe:start::obj`
- count: 1 total / 0 recent
- bytes: p50=7820 p90=7820 max=7820 min=7820
- avg component bytes (top): current=6781, availableStacks=729, progress=170, message=20, sessionId=18, intent=2
- seen in: recipe-laravel-minimal-standard
- sample: samples/workflow-bootstrap-recipe-start-obj.json

## `dev_server:logs`
- count: 4 total / 0 recent
- bytes: p50=811 p90=4592 max=4592 min=161
- avg component bytes (top): logTail=1689, message=61, logFile=25, hostname=8, action=6, running=5
- seen in: classic-go-simple, recipe-nestjs-minimal-standard, recipe-nextjs-ssr-frontend-standard
- sample: samples/dev-server-logs.json

## `dev_server:status`
- count: 7 total / 0 recent
- bytes: p50=169 p90=170 max=173 min=167
- avg component bytes (top): message=49, healthPath=10, action=8, hostname=8, reason=8, running=4, port=4, healthStatus=3
- seen in: api-node-postgres-classic-dev, classic-go-simple, develop-loop-after-bootstrap, recipe-nestjs-minimal-standard
- sample: samples/dev-server-status.json

## `workflow:develop:start::error`
- count: 32 total / 0 recent
- bytes: p50=288 p90=288 max=374 min=247
- avg component bytes (top): suggestion=106, recovery=76, error=34, code=18
- seen in: cross-deploy-stage-promote-from-dev, develop-add-managed-dep-to-existing, existing-standard-appdev-only-reminders, export-buildfromgit-self-snapshot
- sample: samples/workflow-develop-start-error.json

## `workflow:bootstrap/adopt:start::obj`
- count: 2 total / 0 recent
- bytes: p50=4403 p90=4405 max=4405 min=4403
- avg component bytes (top): current=3318, availableStacks=729, progress=170, intent=69, message=20, sessionId=18
- seen in: existing-standard-appdev-only-reminders, verify-subdomain-recovery-before-browser
- sample: samples/workflow-bootstrap-adopt-start-obj.json

## `mount:status`
- count: 5 total / 0 recent
- bytes: p50=666 p90=1152 max=1152 min=204
- avg component bytes (top): mounts=712
- seen in: cross-deploy-stage-promote-from-dev, launch-failure-build-stuck, launch-to-existing-prod-project, verify-subdomain-recovery-before-browser
- sample: samples/mount-status.json

## `mount:mount`
- count: 5 total / 0 recent
- bytes: p50=120 p90=132 max=132 min=120
- avg component bytes (top): message=30, mountPath=14, status=9, hostname=5, writable=4
- seen in: delivery-git-push-actions-setup, kanban-laravel-minimal-dev-only, recover-failed-buildfromgit-missing-dep
- sample: samples/mount-mount.json

## `workflow:bootstrap:discover::route-menu`
- count: 1 total / 0 recent
- bytes: p50=6768 p90=6768 max=6768 min=6768
- avg component bytes (top): routeOptions=4682, routeOptions.importYaml(sum)=3820, message=1150, nextCalls=771, routeOptions.why(sum)=593, intent=120, projectId=24, kind=12
- seen in: greenfield-node-postgres-dev-stage
- sample: samples/workflow-bootstrap-discover-route-menu.json

## `workflow:?:classify::error`
- count: 1 total / 0 recent
- bytes: p50=372 p90=372 max=372 min=372
- avg component bytes (top): suggestion=213, recovery=47, error=42, code=19
- seen in: launch-production-from-standard-pair
- sample: samples/workflow-classify-error.json

## `workflow:launch-production:classify::error`
- count: 4 total / 0 recent
- bytes: p50=372 p90=372 max=372 min=372
- avg component bytes (top): suggestion=213, recovery=47, error=42, code=19
- seen in: launch-production-dev-only, launch-production-from-standard-pair, launch-production-laravel-showcase
- sample: samples/workflow-launch-production-classify-error.json

## `workflow:launch-production:complete::error`
- count: 2 total / 0 recent
- bytes: p50=190 p90=190 max=190 min=190
- avg component bytes (top): recovery=47, suggestion=45, error=38, code=19
- seen in: launch-production-dev-only, launch-production-laravel-showcase
- sample: samples/workflow-launch-production-complete-error.json

## `workflow:launch-production:status::launch-active`
- count: 7 total / 0 recent
- bytes: p50=1967 p90=1979 max=3589 min=1665
- avg component bytes (top): guidance=1612, ambiguousChoices=207, nextCall=87, sourceProjectId=24, lastUpdate=22, workflow=19, launchId=18, status=17, kind=15, targetProjectName=11
- seen in: launch-production-dev-only, launch-production-from-standard-pair, launch-production-laravel-showcase, launch-production-pipeline-not-configured
- sample: samples/workflow-launch-production-status-launch-active.json

## `workflow:?:status::launch-active`
- count: 15 total / 0 recent
- bytes: p50=1967 p90=1979 max=1979 min=1658
- avg component bytes (top): guidance=1339, ambiguousChoices=207, nextCall=85, sourceProjectId=24, targetProjectId=24, lastUpdate=22, workflow=19, launchId=18, kind=15, status=15
- seen in: cross-deploy-stage-promote-from-dev, export-buildfromgit-self-snapshot, greenfield-node-postgres-dev-stage, launch-production-dev-only
- sample: samples/workflow-status-launch-active.json

## `workflow:?:git-push-setup::error`
- count: 61 total / 0 recent
- bytes: p50=452 p90=462 max=551 min=225
- avg component bytes (top): suggestion=229, error=105, recovery=47, code=19
- seen in: export-buildfromgit-self-snapshot, git-push-setup-with-cicd-method-prompt, launch-production-dev-only, launch-production-existing-project-token
- sample: samples/workflow-git-push-setup-error.json

## `workflow:launch-production:start::status=failed`
- count: 18 total / 0 recent
- bytes: p50=496 p90=2207 max=8408 min=456
- avg component bytes (top): blockers=818, guidance=645, phase=26, workflow=19, status=8
- seen in: launch-production-dev-only, launch-production-from-standard-pair, launch-production-laravel-showcase, launch-to-existing-prod-project
- sample: samples/workflow-launch-production-start-status-failed.json

## `workflow:?:build-integration::error`
- count: 11 total / 0 recent
- bytes: p50=287 p90=287 max=287 min=225
- avg component bytes (top): suggestion=91, recovery=89, error=41, code=16
- seen in: launch-production-pipeline-not-configured
- sample: samples/workflow-build-integration-error.json

## `recipe:start`
- count: 1 total / 0 recent
- bytes: p50=8856 p90=8856 max=8856 min=8856
- avg component bytes (top): guidance=8449, status=122, slug=24, parentStatus=8, action=7, ok=4
- seen in: kanban-laravel-minimal-dev-only
- sample: samples/recipe-start.json

## `recipe:update-plan`
- count: 1 total / 0 recent
- bytes: p50=187 p90=187 max=187 min=187
- avg component bytes (top): status=122, slug=24, action=13, ok=4
- seen in: kanban-laravel-minimal-dev-only
- sample: samples/recipe-update-plan.json

## `recipe:complete-phase`
- count: 5 total / 0 recent
- bytes: p50=6876 p90=13151 max=13151 min=213
- avg component bytes (top): guidance=8929, violations=2103, status=145, slug=24, action=16, ok=4
- seen in: kanban-laravel-minimal-dev-only
- sample: samples/recipe-complete-phase.json

## `recipe:enter-phase`
- count: 2 total / 0 recent
- bytes: p50=7991 p90=13123 max=13123 min=7991
- avg component bytes (top): guidance=10114, status=139, slug=24, action=13, ok=4
- seen in: kanban-laravel-minimal-dev-only
- sample: samples/recipe-enter-phase.json

## `recipe:emit-yaml`
- count: 1 total / 0 recent
- bytes: p50=518 p90=518 max=518 min=518
- avg component bytes (top): yaml=446, slug=24, action=11, ok=4
- seen in: kanban-laravel-minimal-dev-only
- sample: samples/recipe-emit-yaml.json

## `recipe:build-subagent-prompt`
- count: 1 total / 0 recent
- bytes: p50=265 p90=265 max=265 min=265
- avg component bytes (top): briefPath=90, notice=58, slug=24, action=23, briefSize=5, ok=4
- seen in: kanban-laravel-minimal-dev-only
- sample: samples/recipe-build-subagent-prompt.json

## `recipe:record-fact`
- count: 12 total / 0 recent
- bytes: p50=66 p90=66 max=425 min=66
- avg component bytes (top): notice=349, slug=24, action=13, ok=4
- seen in: kanban-laravel-minimal-dev-only
- sample: samples/recipe-record-fact.json

## `workflow:bootstrap/recipe:start::error`
- count: 2 total / 0 recent
- bytes: p50=326 p90=326 max=326 min=326
- avg component bytes (top): suggestion=125, error=96, recovery=47, code=17
- seen in: kanban-laravel-minimal-dev-only
- sample: samples/workflow-bootstrap-recipe-start-error.json

## `manage:reload`
- count: 3 total / 0 recent
- bytes: p50=325 p90=325 max=325 min=325
- avg component bytes (top): nextActions=53, serviceStacks=52, created=26, started=26, finished=26, id=24, actionName=14, status=10
- seen in: git-push-setup-then-actions, launch-production-existing-with-webhook, launch-production-from-standard-pair
- sample: samples/manage-reload.json

## `workflow:launch-production:start::error`
- count: 4 total / 0 recent
- bytes: p50=408 p90=423 max=423 min=177
- avg component bytes (top): apiMeta=107, suggestion=61, recovery=47, error=33, apiCode=17, code=13
- seen in: launch-production-from-standard-pair, launch-to-existing-prod-project
- sample: samples/workflow-launch-production-start-error.json

## `workflow:launch-production:start::status=launching`
- count: 5 total / 0 recent
- bytes: p50=178 p90=178 max=178 min=178
- avg component bytes (top): guidance=78, phase=26, workflow=19, status=11
- seen in: launch-to-existing-prod-project
- sample: samples/workflow-launch-production-start-status-launching.json

## `workflow:launch-production:list::nondict`
- count: 1 total / 0 recent
- bytes: p50=2 p90=2 max=2 min=2
- seen in: launch-production-pipeline-not-configured
- sample: samples/workflow-launch-production-list-nondict.json

## `workflow:?:adopt-local::error`
- count: 1 total / 0 recent
- bytes: p50=182 p90=182 max=182 min=182
- avg component bytes (top): error=89, recovery=47, code=19
- seen in: git-push-setup-then-actions
- sample: samples/workflow-adopt-local-error.json

## `workflow:launch-production:reset::error`
- count: 1 total / 0 recent
- bytes: p50=607 p90=607 max=607 min=607
- avg component bytes (top): suggestion=246, wouldDestroy=164, error=69, recovery=47, code=20
- seen in: launch-production-from-standard-pair
- sample: samples/workflow-launch-production-reset-error.json

## `workflow:launch-production:reset::obj`
- count: 2 total / 0 recent
- bytes: p50=276 p90=358 max=358 min=276
- avg component bytes (top): note=88, deletedStateFile=31, operation=25, sourceProjectId=24, launchId=18, targetProjectName=12, priorStatus=9
- seen in: launch-production-from-standard-pair
- sample: samples/workflow-launch-production-reset-obj.json

## `manage:restart`
- count: 1 total / 0 recent
- bytes: p50=326 p90=326 max=326 min=326
- avg component bytes (top): nextActions=53, serviceStacks=52, created=26, started=26, finished=26, id=24, actionName=15, status=10
- seen in: cadence-multiservice-build-run2-replay
- sample: samples/manage-restart.json

## `workflow:?:build-integration::status=noop`
- count: 2 total / 0 recent
- bytes: p50=291 p90=297 max=297 min=291
- avg component bytes (top): workSessionState=215, buildIntegration=7, service=6, status=6
- seen in: git-push-setup-then-actions, launch-production-dev-only
- sample: samples/workflow-build-integration-status-noop.json

## `workflow:?:set-default-setup::error`
- count: 1 total / 0 recent
- bytes: p50=268 p90=268 max=268 min=268
- avg component bytes (top): suggestion=98, error=53, recovery=47, code=19
- seen in: stale-setup-rename-recovery
- sample: samples/workflow-set-default-setup-error.json

## `workflow:?:set-default-setup::status=configured`
- count: 1 total / 0 recent
- bytes: p50=91 p90=91 max=91 min=91
- avg component bytes (top): status=12, service=8, stageSetupName=6, primarySetupName=5
- seen in: stale-setup-rename-recovery
- sample: samples/workflow-set-default-setup-status-configured.json

## `workflow:?:skip::error`
- count: 1 total / 0 recent
- bytes: p50=267 p90=267 max=267 min=267
- avg component bytes (top): error=94, suggestion=63, recovery=47, code=22
- seen in: recipe-laravel-showcase-fullstack
- sample: samples/workflow-skip-error.json

## `workflow:bootstrap/adopt:start::error`
- count: 1 total / 0 recent
- bytes: p50=356 p90=356 max=356 min=356
- avg component bytes (top): suggestion=147, error=102, recovery=47, code=19
- seen in: launch-production-from-standard-pair
- sample: samples/workflow-bootstrap-adopt-start-error.json

## `workflow:export:start::error`
- count: 5 total / 0 recent
- bytes: p50=384 p90=384 max=384 min=248
- avg component bytes (top): error=111, suggestion=107, recovery=47, code=19
- seen in: export-buildfromgit-self-snapshot
- sample: samples/workflow-export-start-error.json