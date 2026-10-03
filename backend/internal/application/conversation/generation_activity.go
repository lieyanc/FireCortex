package conversation

// HasActiveGenerations 报告当前进程是否有进行中的消息生成，供自更新在重启前等待空闲。
func (s *Service) HasActiveGenerations() bool {
	if s == nil || s.generationStreams == nil {
		return false
	}
	return s.generationStreams.activeCount() > 0
}

func (r *generationStreamRegistry) activeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active)
}
