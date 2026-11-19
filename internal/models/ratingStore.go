package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

// RatingStore owns mod_ratings and the denormalised aggregates on mods.
type RatingStore struct {
	db *sql.DB
}

// Rate stores the rating of a user and refreshes the aggregates of the mod in
// the same transaction, so a sort by rating never reads a stale value.
func (s *RatingStore) Rate(ctx context.Context, userID int64, modID int64, rating int64) (*Ratings, error) {
	if userID <= 0 {
		return nil, ErrUnauthorized
	}
	if rating < 1 || rating > 5 {
		return nil, ErrValidation
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin rate: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mods WHERE modId = ?`, modID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check mod: %w", err)
	}
	if exists == 0 {
		return nil, ErrNotFound
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO mod_ratings (userId, modId, rating) VALUES (?, ?, ?)
		 ON CONFLICT (userId, modId) DO UPDATE SET rating = excluded.rating`,
		userID, modID, rating); err != nil {
		return nil, fmt.Errorf("upsert rating: %w", err)
	}

	ratings, err := recomputeTx(ctx, tx, modID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit rate: %w", err)
	}
	return ratings, nil
}

// Recompute refreshes the aggregates of one mod.
func (s *RatingStore) Recompute(ctx context.Context, modID int64) (*Ratings, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin recompute: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ratings, err := recomputeTx(ctx, tx, modID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit recompute: %w", err)
	}
	return ratings, nil
}

// recomputeTx recalculates the rating aggregates of a mod and writes them back.
func recomputeTx(ctx context.Context, tx *sql.Tx, modID int64) (*Ratings, error) {
	var total int64
	var average sql.NullFloat64
	var positive sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*), AVG(rating),
			SUM(CASE WHEN rating >= 4 THEN 1 ELSE 0 END)
		 FROM mod_ratings WHERE modId = ?`, modID).
		Scan(&total, &average, &positive)
	if err != nil {
		return nil, fmt.Errorf("aggregate ratings: %w", err)
	}

	var reviewsCount int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reviews WHERE modId = ?`, modID).
		Scan(&reviewsCount); err != nil {
		return nil, fmt.Errorf("count reviews: %w", err)
	}

	// The render commits to the one decimal the capture shows: the AOM Rate
	// answers average 4.1 for a mod of 125 ratings, so the stored column and
	// both renders carry the rounded form.
	average.Float64 = math.Round(average.Float64*10) / 10

	if _, err := tx.ExecContext(ctx,
		`UPDATE mods SET currentRating = ?, positiveRatings = ?, totalRatings = ?, reviewsCount = ?
		 WHERE modId = ?`,
		average.Float64, positive.Int64, total, reviewsCount, modID); err != nil {
		return nil, fmt.Errorf("update mod aggregates: %w", err)
	}

	return &Ratings{
		Rating: RatingBreakdown{
			Average:    average.Float64,
			TotalCount: total,
		},
		ReviewsCount: reviewsCount,
	}, nil
}

// AddReview inserts a review and refreshes the aggregates.
func (s *RatingStore) AddReview(ctx context.Context, review Review, modID int64) error {
	if review.Rating < 1 || review.Rating > 5 {
		return ErrValidation
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin add review: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mods WHERE modId = ?`, modID).Scan(&exists); err != nil {
		return fmt.Errorf("check mod: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO reviews (reviewId, modId, reviewerId, reviewerName, avatarUrl, rating, title,
			reviewText, submitted, itemVersion, locale, helpfulPositive, helpfulNegative,
			helpfulnessVotes, isInstalled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		review.ReviewID, modID, review.ReviewerID, review.ReviewerName, review.AvatarURL,
		review.Rating, review.Title, review.ReviewText, review.Submitted, review.ItemVersion,
		review.Locale, review.HelpfulPositive, review.HelpfulNegative, review.HelpfulnessVotes,
		BoolToNumber(review.IsInstalled))
	if err != nil {
		return fmt.Errorf("insert review: %w", err)
	}
	if _, err := recomputeTx(ctx, tx, modID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit add review: %w", err)
	}
	return nil
}

// ErrNoRating reports that the user has not rated the mod.
var ErrNoRating = errors.New("no rating")
