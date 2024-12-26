package pp241007

import (
	"math"

	"github.com/wieku/danser-go/app/beatmap/difficulty"
	"github.com/wieku/danser-go/app/rulesets/osu/performance/api"
	"github.com/wieku/danser-go/app/rulesets/osu/performance/pp241007/skills"
)

const (
	PerformanceBaseMultiplier float64 = 1.09
)

/* ------------------------------------------------------------- */
/* pp calc                                                       */

// PPv2 : structure to store ppv2 values
type PPv2 struct {
	attribs api.Attributes

	score api.PerfScore

	diff *difficulty.Difficulty

	effectiveMissCount           float64
	totalHits                    int
	totalImperfectHits           int
	countSliderEndsDropped       int
	amountHitObjectsWithAccuracy int

	usingClassicSliderAccuracy bool
}

func NewPPCalculator() api.IPerformanceCalculator {
	return &PPv2{}
}

func (pp *PPv2) Calculate(attribs api.Attributes, score api.PerfScore, diff *difficulty.Difficulty) api.PPv2Results {
	attribs.MaxCombo = max(1, attribs.MaxCombo)

	if score.MaxCombo < 0 {
		score.MaxCombo = attribs.MaxCombo
	}

	if score.CountGreat < 0 {
		score.CountGreat = attribs.ObjectCount - score.CountOk - score.CountMeh - score.CountMiss
	}

	pp.usingClassicSliderAccuracy = !diff.CheckModActive(difficulty.Lazer)

	if diff.CheckModActive(difficulty.Lazer) && diff.CheckModActive(difficulty.Classic) {
		if conf, ok := difficulty.GetModConfig[difficulty.ClassicSettings](diff); ok {
			pp.usingClassicSliderAccuracy = conf.NoSliderHeadAccuracy
		}
	}

	pp.attribs = attribs
	pp.diff = diff
	pp.score = score

	pp.countSliderEndsDropped = attribs.Sliders - score.SliderEnd
	pp.totalHits = score.CountGreat + score.CountOk + score.CountMeh + score.CountMiss
	pp.totalImperfectHits = score.CountOk + score.CountMeh + score.CountMiss
	pp.effectiveMissCount = 0

	if pp.attribs.Sliders > 0 {
		if pp.usingClassicSliderAccuracy {
			// Consider that full combo is maximum combo minus dropped slider tails since they don't contribute to combo but also don't break it
			// In classic scores we can't know the amount of dropped sliders so we estimate to 10% of all sliders on the map
			fullComboThreshold := float64(pp.attribs.MaxCombo) - 0.1*float64(pp.attribs.Sliders)

			if float64(pp.score.MaxCombo) < fullComboThreshold {
				pp.effectiveMissCount = fullComboThreshold / max(1.0, float64(pp.score.MaxCombo))
			}

			pp.effectiveMissCount = min(pp.effectiveMissCount, float64(pp.totalImperfectHits))
		} else {
			fullComboThreshold := float64(pp.attribs.MaxCombo - pp.countSliderEndsDropped)

			if float64(pp.score.MaxCombo) < fullComboThreshold {
				pp.effectiveMissCount = fullComboThreshold / max(1.0, float64(pp.score.MaxCombo))
			}

			// Combine regular misses with tick misses since tick misses break combo as well
			pp.effectiveMissCount = min(pp.effectiveMissCount, float64(pp.score.SliderBreaks+pp.score.CountMiss))
		}

	}

	pp.effectiveMissCount = max(float64(pp.score.CountMiss), pp.effectiveMissCount)

	pp.amountHitObjectsWithAccuracy = attribs.Circles
	if !pp.usingClassicSliderAccuracy {
		pp.amountHitObjectsWithAccuracy += attribs.Sliders
	}

	// total pp

	multiplier := PerformanceBaseMultiplier

	if diff.Mods.Active(difficulty.SpunOut) && pp.totalHits > 0 {
		multiplier *= 1.0 - math.Pow(float64(attribs.Spinners)/float64(pp.totalHits), 0.85)
	}

	accDepression := 1.0

	streamsNerf := math.Round((pp.attribs.Aim/pp.attribs.Speed)*100) / 100

	results := api.PPv2Results{
		Aim:        pp.computeAimValue(),
		Speed:      pp.computeSpeedValue(),
		Acc:        pp.computeAccuracyValue(),
		Flashlight: pp.computeFlashlightValue(),
	}

	if streamsNerf < 1.09 {
		accFactor := math.Abs(1 - pp.score.Accuracy)
		accDepression = math.Max(0.86-accFactor, 0.5)
		if accDepression > 0.0 {
			results.Aim *= accDepression
		}
	}

	results.Total = math.Pow(
		math.Pow(results.Aim, 1.185)+
			math.Pow(results.Speed, 0.83*accDepression)+
			math.Pow(results.Acc, 1.14),
		1.0/1.1) * multiplier

	return results
}

func (pp *PPv2) computeAimValue() float64 {

	aimValue := skills.DefaultDifficultyToPerformance(pp.attribs.Aim)
	// Longer maps are worth more
	lengthBonus := 0.88 + 0.4*min(1.0, float64(pp.totalHits)/2000.0)
	if pp.totalHits > 2000 {
		lengthBonus += math.Log10(float64(pp.totalHits)/2000.0) * 0.5
	}

	aimValue *= lengthBonus

	// Penalize misses by assessing # of misses relative to the total # of objects. Default a 3% reduction for any # of misses.
	if pp.effectiveMissCount > 0 {
		aimValue *= pp.calculateMissPenalty(pp.effectiveMissCount)
	}

	approachRateFactor := 0.0
	if pp.diff.ARReal > 10.33 {
		approachRateFactor = 0.3 * (pp.diff.ARReal - 10.33)
	} else if pp.diff.ARReal < 8.0 {
		approachRateFactor = 0.025 * (8.0 - pp.diff.ARReal)
	}

	aimValue *= 1.0 + approachRateFactor*lengthBonus

	// We want to give more reward for lower AR when it comes to aim and HD. This nerfs high AR and buffs lower AR.
	if pp.diff.Mods.Active(difficulty.Hidden) {
		aimValue *= 1.0 + 0.05*(11.0-pp.diff.ARReal)
	}

	// FL Bonus
	if pp.diff.Mods.Active(difficulty.Flashlight) {
		aimValue *= 1.0 + math.Min(0.3*(float64(pp.totalHits)/200), 1)
		if pp.totalHits > 200 {
			aimValue += (0.25 * math.Min((float64(pp.totalHits)-200)/300, 1))
		}
		if pp.totalHits > 500 {
			aimValue += ((float64(pp.totalHits) - 500) / 1600)
		}
	}

	aimValue *= 0.3 + pp.score.Accuracy/2
	// It is important to also consider accuracy difficulty when doing that
	aimValue *= 0.98 + math.Pow(pp.diff.ODReal, 2)/2500

	return aimValue
}

func (pp *PPv2) computeSpeedValue() float64 {
	if pp.diff.CheckModActive(difficulty.Relax) {
		return 0
	}

	speedValue := skills.DefaultDifficultyToPerformance(pp.attribs.Speed)

	// Longer maps are worth more
	lengthBonus := 0.88 + 0.4*min(1.0, float64(pp.totalHits)/2000.0)
	if pp.totalHits > 2000 {
		lengthBonus += math.Log10(float64(pp.totalHits)/2000.0) * 0.5
	}

	speedValue *= lengthBonus

	// Penalize misses by assessing # of misses relative to the total # of objects. Default a 3% reduction for any # of misses.
	if pp.effectiveMissCount > 0 {
		speedValue *= pp.calculateMissPenalty(pp.effectiveMissCount)
	}

	approachRateFactor := 0.0
	if pp.diff.ARReal > 10.33 {
		approachRateFactor = 0.3 * (pp.diff.ARReal - 10.33)
	} else if pp.diff.ARReal < 8 {
		approachRateFactor = 0.025 * (8 - pp.diff.ARReal)
	}

	speedValue *= 1.0 + approachRateFactor*lengthBonus

	if pp.diff.Mods.Active(difficulty.Hidden) {
		speedValue *= 1.0 + 0.05*(11.0-pp.diff.ARReal)
	}

	speedValue *= (0.93 + math.Pow(pp.diff.ODReal, 2)/750) * math.Pow(pp.score.Accuracy, 14.5-math.Max(pp.diff.ODReal, 8)/2)
	if float64(pp.score.CountMeh) > float64(pp.totalHits)/500 {
		speedValue *= float64(pp.score.CountMeh) - float64(pp.totalHits)/500
	}

	return speedValue
}

func (pp *PPv2) computeAccuracyValue() float64 {
	// This percentage only considers HitCircles of any value - in this part of the calculation we focus on hitting the timing hit window
	betterAccuracyPercentage := 0.0

	if pp.amountHitObjectsWithAccuracy > 0 {
		betterAccuracyPercentage = float64((pp.score.CountGreat-(pp.totalHits-pp.amountHitObjectsWithAccuracy))*6+pp.score.CountOk*2+pp.score.CountMeh) / (float64(pp.amountHitObjectsWithAccuracy) * 6)
	}

	// It is possible to reach a negative accuracy with this formula. Cap it at zero - zero points
	if betterAccuracyPercentage < 0 {
		betterAccuracyPercentage = 0
	}

	// Lots of arbitrary values from testing.
	// Considering to use derivation from perfect accuracy in a probabilistic manner - assume normal distribution
	accuracyValue := math.Pow(1.52163, pp.diff.ODReal) * math.Pow(betterAccuracyPercentage, 24) * 2.83

	// Bonus for many hitcircles - it's harder to keep good accuracy up for longer
	accuracyValue *= min(1.15, math.Pow(float64(pp.amountHitObjectsWithAccuracy)/1000.0, 0.3))

	if pp.diff.Mods.Active(difficulty.Hidden) {
		accuracyValue *= 1.08
	}

	if pp.diff.Mods.Active(difficulty.Flashlight) {
		accuracyValue *= 1.02
	}

	return accuracyValue
}

func (pp *PPv2) computeFlashlightValue() float64 {
	return 0
}

func (pp *PPv2) calculateMissPenalty(missCount float64) float64 {
	return 0.97 * (1 - math.Pow(math.Pow(missCount/float64(pp.totalHits), 0.5), 1+(missCount/1.5)))
}

func (pp *PPv2) getComboScalingFactor() float64 {
	if pp.attribs.MaxCombo <= 0 {
		return 1.0
	} else {
		return min(math.Pow(float64(pp.score.MaxCombo), 0.8)/math.Pow(float64(pp.attribs.MaxCombo), 0.8), 1.0)
	}
}
