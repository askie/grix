import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../controllers/paged_list_controller.dart';
import 'async_view.dart';

/// 无限滚动列表：自动检测滚动到底部触发 loadMore。
///
/// 首次加载使用 [AsyncView] 展示 loading/error/empty 状态，
/// 滚动到底部时通过 [PagedListController.loadMore] 追加数据，
/// 列表末尾显示加载指示器。
class InfiniteListView<T> extends StatefulWidget {
  const InfiniteListView({
    super.key,
    required this.controller,
    required this.itemBuilder,
    this.emptyText = '暂无数据',
    this.padding,
  });

  final PagedListController<T> controller;
  final Widget Function(BuildContext context, T item, int index) itemBuilder;
  final String emptyText;
  final EdgeInsetsGeometry? padding;

  @override
  State<InfiniteListView<T>> createState() => _InfiniteListViewState<T>();
}

class _InfiniteListViewState<T> extends State<InfiniteListView<T>> {
  final _scrollController = ScrollController();

  @override
  void initState() {
    super.initState();
    _scrollController.addListener(_onScroll);
  }

  @override
  void dispose() {
    _scrollController.removeListener(_onScroll);
    _scrollController.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (!_scrollController.hasClients) return;
    final maxScroll = _scrollController.position.maxScrollExtent;
    final currentScroll = _scrollController.position.pixels;
    // 距底部不足 200px 时触发加载
    if (maxScroll - currentScroll < 200) {
      widget.controller.loadMore();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Obx(() => AsyncView(
          loading: widget.controller.loading.value,
          error: widget.controller.error.value,
          isEmpty: widget.controller.items.isEmpty,
          onRetry: widget.controller.reload,
          emptyText: widget.emptyText,
          builder: (_) => ListView.builder(
            controller: _scrollController,
            padding: widget.padding ??
                const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
            itemCount: widget.controller.items.length + 1,
            itemBuilder: (context, index) {
              // 最后一项：加载指示器或空白
              if (index == widget.controller.items.length) {
                return Obx(() => Padding(
                      padding: const EdgeInsets.symmetric(vertical: 12),
                      child: Center(
                        child: widget.controller.loadingMore.value
                            ? const SizedBox(
                                width: 20,
                                height: 20,
                                child: CircularProgressIndicator(strokeWidth: 2),
                              )
                            : const SizedBox.shrink(),
                      ),
                    ));
              }
              return Column(
                children: [
                  widget.itemBuilder(
                      context, widget.controller.items[index], index),
                  if (index < widget.controller.items.length - 1)
                    const SizedBox(height: 8),
                ],
              );
            },
          ),
        ));
  }
}
