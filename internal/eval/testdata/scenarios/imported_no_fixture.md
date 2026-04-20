---
id: test-imported-no-fixture
description: Imported seed without fixture path
seed: imported
expect:
  mustCallTools:
    - zerops_workflow
---

# Task

Should fail — imported requires fixture.
