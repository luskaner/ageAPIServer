package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// PollStore owns polls, their questions and options.
type PollStore struct {
	db *sql.DB
}

// List returns the polls that have not expired yet.
func (s *PollStore) List(ctx context.Context) ([]PollSummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pollId, gameId, created, startDateTime, endDateTime, expireDateTime, metadata, icon
		 FROM polls ORDER BY startDateTime DESC`)
	if err != nil {
		return nil, fmt.Errorf("query polls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	polls := make([]PollSummary, 0, 8)
	for rows.Next() {
		var poll PollSummary
		var icon sql.NullString
		err := rows.Scan(&poll.ID, &poll.GameID, &poll.Created, &poll.StartDateTime,
			&poll.EndDateTime, &poll.ExpireDateTime, &poll.Metadata, &icon)
		if err != nil {
			return nil, fmt.Errorf("scan poll: %w", err)
		}
		poll.Icon = nullString(icon)
		polls = append(polls, poll)
	}
	return polls, rows.Err()
}

// Get returns one poll with its questions, options and the votes of the viewer.
func (s *PollStore) Get(ctx context.Context, pollID string, viewer Viewer) (*Poll, error) {
	pollID = strings.ToLower(strings.TrimSpace(pollID))
	if pollID == "" {
		return nil, ErrValidation
	}

	poll := Poll{ID: pollID, PollID: pollID}
	var createdBy sql.NullString
	var isDefinition int64
	err := s.db.QueryRowContext(ctx,
		`SELECT gameId, createdBy, created, startDateTime, endDateTime, expireDateTime,
			metadata, isDefinition, totalVotes
		 FROM polls WHERE pollId = ?`, pollID).
		Scan(&poll.GameID, &createdBy, &poll.Created, &poll.StartDateTime, &poll.EndDateTime,
			&poll.ExpireDateTime, &poll.Metadata, &isDefinition, &poll.TotalVotes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query poll: %w", err)
	}
	poll.CreatedBy = createdBy.String
	poll.IsDefinition = NumberToBool(isDefinition)

	questions, err := s.questions(ctx, pollID, viewer)
	if err != nil {
		return nil, err
	}
	poll.Questions = questions
	return &poll, nil
}

func (s *PollStore) questions(ctx context.Context, pollID string, viewer Viewer) ([]PollQuestion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, sortOrder, content, type, maxOptions, totalVotes
		 FROM poll_questions WHERE pollId = ? ORDER BY sortOrder, id`, pollID)
	if err != nil {
		return nil, fmt.Errorf("query poll questions: %w", err)
	}
	// Materialise before the nested option query: one connection is shared.
	questions := make([]PollQuestion, 0, 4)
	for rows.Next() {
		var question PollQuestion
		err := rows.Scan(&question.ID, &question.Order, &question.Content, &question.Type,
			&question.MaxOptions, &question.TotalVotes)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan poll question: %w", err)
		}
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate poll questions: %w", err)
	}
	_ = rows.Close()

	votes := map[int64]int64{}
	if viewer.LoggedOn && viewer.UserID > 0 {
		voteRows, err := s.db.QueryContext(ctx,
			`SELECT v.questionId, v.optionId FROM poll_votes v
			 JOIN poll_questions q ON q.id = v.questionId
			 WHERE q.pollId = ? AND v.userId = ?`, pollID, viewer.UserID)
		if err != nil {
			return nil, fmt.Errorf("query poll votes: %w", err)
		}
		for voteRows.Next() {
			var questionID, optionID int64
			if err := voteRows.Scan(&questionID, &optionID); err != nil {
				_ = voteRows.Close()
				return nil, fmt.Errorf("scan poll vote: %w", err)
			}
			votes[questionID] = optionID
		}
		if err := voteRows.Err(); err != nil {
			_ = voteRows.Close()
			return nil, fmt.Errorf("iterate poll votes: %w", err)
		}
		_ = voteRows.Close()
	}

	for i := range questions {
		options, err := s.options(ctx, questions[i].ID, votes[questions[i].ID])
		if err != nil {
			return nil, err
		}
		questions[i].Options = options
		if optionID, ok := votes[questions[i].ID]; ok && optionID > 0 {
			questions[i].UserVoted = true
		}
	}
	return questions, nil
}

func (s *PollStore) options(ctx context.Context, questionID int64, userOption int64) ([]PollOption, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, value, voteCount FROM poll_options WHERE questionId = ? ORDER BY id`, questionID)
	if err != nil {
		return nil, fmt.Errorf("query poll options: %w", err)
	}
	defer func() { _ = rows.Close() }()

	options := make([]PollOption, 0, 4)
	for rows.Next() {
		var option PollOption
		if err := rows.Scan(&option.ID, &option.Value, &option.VoteCount); err != nil {
			return nil, fmt.Errorf("scan poll option: %w", err)
		}
		option.UserVote = userOption > 0 && option.ID == userOption
		options = append(options, option)
	}
	return options, rows.Err()
}
