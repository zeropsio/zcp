package projection

func Compare(left, right *View) Comparison {
	comparison := Comparison{LeftID: left.Capture.ID, RightID: right.Capture.ID, Metrics: []MetricDelta{}}
	rightByID := make(map[string]Metric, len(right.Metrics))
	for _, metric := range right.Metrics {
		rightByID[metric.ID] = metric
	}
	seen := make(map[string]bool, len(left.Metrics))
	for _, leftMetric := range left.Metrics {
		rightMetric, ok := rightByID[leftMetric.ID]
		if !ok {
			rightMetric = Metric{ID: leftMetric.ID, Name: leftMetric.Name, Unit: leftMetric.Unit, EvidenceBasis: leftMetric.EvidenceBasis, MissingCount: 1}
		}
		comparison.Metrics = append(comparison.Metrics, compareMetric(leftMetric, rightMetric))
		seen[leftMetric.ID] = true
	}
	for _, rightMetric := range right.Metrics {
		if seen[rightMetric.ID] {
			continue
		}
		leftMetric := Metric{ID: rightMetric.ID, Name: rightMetric.Name, Unit: rightMetric.Unit, EvidenceBasis: rightMetric.EvidenceBasis, MissingCount: 1}
		comparison.Metrics = append(comparison.Metrics, compareMetric(leftMetric, rightMetric))
	}
	return comparison
}

func compareMetric(left, right Metric) MetricDelta {
	metric := MetricDelta{
		ID: left.ID, Name: left.Name, Unit: left.Unit,
		Left: copyMetricNumber(left.Value), Right: copyMetricNumber(right.Value),
		LeftMissingCount: left.MissingCount, RightMissingCount: right.MissingCount,
		EvidenceBasis: left.EvidenceBasis,
	}
	if metric.Name == "" {
		metric.Name = right.Name
	}
	if metric.Unit == "" {
		metric.Unit = right.Unit
	}
	if metric.EvidenceBasis == "" {
		metric.EvidenceBasis = right.EvidenceBasis
	}
	if metric.Left != nil && metric.Right != nil {
		delta := *metric.Right - *metric.Left
		metric.Delta = &delta
		if *metric.Left != 0 {
			percent := delta / *metric.Left * 100
			metric.Percent = &percent
		}
	}
	return metric
}

func copyMetricNumber(value *float64) *float64 {
	if value == nil {
		return nil
	}
	valueCopy := *value
	return &valueCopy
}
