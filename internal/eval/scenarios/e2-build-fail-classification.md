---
id: e2-build-fail-classification
description: E2 verification — agent triggers a known BUILD_FAILED and reports the structured failureClassification block from the deploy response
seed: imported
fixture: fixtures/laravel-minimal.yaml
preseedScript: preseed/first-deploy-branch.sh
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_deploy
  workflowCallsMin: 1
  mustEnterWorkflow:
    - develop
  requiredPatterns:
    - '"workflow":"develop"'
    - '"scope":['
    - '"appdev"'
    # Ticket E2 contract: every BUILD_FAILED response carries failureClassification.
    # The pattern matches anywhere in the eval log (tool responses + assessment).
    - '"failureClassification"'
    - '"category":"build"'
    - '"build:command-not-found"'
  forbiddenPatterns:
    - '"workflow":"bootstrap","intent"'
  # Intentional-failure scenario — agent MUST NOT report State: SUCCESS;
  # the assessment should describe the build failure + the structured
  # failureClassification the agent observed.
  requireAssessment: false
followUp:
  - "Co konkrétně obsahoval `failureClassification` block v odpovědi `zerops_deploy`? Vyjmenuj `category`, `likelyCause`, `suggestedAction` a `signals`."
  - "Pomohlo ti `failureClassification` rychleji najít root cause oproti čtení `buildLogs` ručně?"
---

# Úkol

V projektu je provisioned služba `appdev` (php-nginx@8.4) — služba existuje
ale ještě nikdy nebyla deployed. Tvým úkolem je:

1. **Začni s `zerops_workflow action="status"`** abys zjistil stav.
2. Začni develop workflow (scope `appdev`).
3. Vytvoř minimální Laravel-style aplikaci s **úmyslně rozbitým**
   `zerops.yml` — buildCommand má nesmyslný binárek, který v build
   containeru neexistuje. Deploy MUSÍ selhat ve build phase.

   Použij přesně tento `zerops.yml`:

   ```yaml
   zerops:
     - setup: appdev
       build:
         base: php@8.4
         buildCommands:
           - echo "starting build"
           - thisbinaryisnotreal_e2_xyz_abc
         deployFiles: ./
       run:
         base: php-nginx@8.4
         documentRoot: public
   ```

   A minimální `public/index.php`:

   ```php
   <?php echo "ok";
   ```

4. Zavolej `zerops_deploy targetService="appdev"`. Očekáváš `BUILD_FAILED`.

5. **Z odpovědi `zerops_deploy`** přečti pole `failureClassification` —
   konkrétně `category`, `likelyCause`, `suggestedAction` a `signals`.
   Tato pole jsou **primární signál** pro recovery, nemusíš parsovat
   `buildLogs` ručně.

6. V assessmentu na konci uveď:
   - hodnotu `status` field
   - hodnotu `failureClassification.category`
   - krátký výpis `failureClassification.likelyCause` (1 věta)
   - hodnotu `failureClassification.suggestedAction` (1 věta)

Pravidla:

- Začni `zerops_workflow action="status"` a řiď se vráceným next-action.
- Bootstrap NEPOUŽÍVEJ — služba je už bootstrapped, jdi rovnou do develop.
- Deploy MUSÍ selhat (úmyslné) — to je celý smysl scénáře. Nesnaž se
  zerops.yml opravovat, pouze ji deploy a report.

Verify: agent assessment obsahuje hodnoty `failureClassification.category`
a `failureClassification.suggestedAction` z deploy response.
